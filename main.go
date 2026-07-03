// skilldrift は導入済みClaude Code Skillsの上流変更を監視するCLI。
// lockfile(skilldrift.lock)に記録した出自と上流の現在状態を比較し、ドリフトを検知する。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kpab/skilldrift/internal/lockfile"
	"github.com/kpab/skilldrift/internal/scan"
	"github.com/kpab/skilldrift/internal/upstream"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "init":
		err = runInit(os.Args[2:])
	case "check":
		drifted, cerr := runCheck(os.Args[2:])
		if cerr != nil {
			fmt.Fprintln(os.Stderr, "skilldrift:", cerr)
			os.Exit(2)
		}
		if drifted {
			os.Exit(1)
		}
		return
	case "version":
		fmt.Println("skilldrift", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "skilldrift: 不明なサブコマンド %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "skilldrift:", err)
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `skilldrift — Dependabot for Claude Code Skills

Usage:
  skilldrift <command> [flags]

Commands:
  init     スキルの出自を skilldrift.lock に記録する
  check    lockfileと上流を比較しドリフトを検知する
  version  バージョンを表示する

checkの終了コード: 0=ドリフトなし / 1=ドリフト検知 / 2=エラー
`)
}

func runInit(args []string) error {
	fset := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := fset.String("dir", ".", "走査するリポジトリルート")
	if err := fset.Parse(args); err != nil {
		return err
	}

	found, err := scan.Skills(*dir)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return fmt.Errorf("init: %s 以下にスキル(SKILL.mdを含むディレクトリ)が見つからない", *dir)
	}

	lockPath := filepath.Join(*dir, lockfile.DefaultPath)
	lf, err := lockfile.Load(lockPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		lf = lockfile.New()
	case err != nil:
		return err
	}

	ch := lf.Reconcile(found)
	if err := lf.Save(lockPath); err != nil {
		return err
	}

	fmt.Printf("%s を書き出した: スキル%d件(新規%d・削除%d)\n", lockPath, len(lf.Skills), len(ch.Added), len(ch.Removed))
	for _, name := range ch.Removed {
		fmt.Printf("  削除: %s(ローカルに存在しない)\n", name)
	}
	var untracked []lockfile.Skill
	for _, s := range lf.Skills {
		if !s.Tracked() {
			untracked = append(untracked, s)
		}
	}
	if len(untracked) > 0 {
		fmt.Printf("\n出自未記入のスキルが%d件ある。%s の source.repo(owner/name)と commit を記入すると監視対象になる:\n", len(untracked), lockPath)
		for _, s := range untracked {
			fmt.Printf("  - %s (%s)\n", s.Name, s.Path)
		}
	}
	return nil
}

// runCheck はlockfileと上流を比較する。返り値はドリフト有無とエラー。
func runCheck(args []string) (bool, error) {
	return runCheckWith(upstream.New(), args)
}

func runCheckWith(client *upstream.Client, args []string) (bool, error) {
	fset := flag.NewFlagSet("check", flag.ContinueOnError)
	dir := fset.String("dir", ".", "lockfileのあるリポジトリルート")
	if err := fset.Parse(args); err != nil {
		return false, err
	}

	lockPath := filepath.Join(*dir, lockfile.DefaultPath)
	lf, err := lockfile.Load(lockPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("check: %s が無い。先に skilldrift init を実行してください", lockPath)
	}
	if err != nil {
		return false, err
	}

	ctx := context.Background()
	resolved := map[string]string{} // "repo@ref" -> commit SHA(同一上流への問い合わせは1回)
	var driftCount, untracked, failures int
	for _, s := range lf.Skills {
		if !s.Tracked() {
			untracked++
			continue
		}
		key := s.Source.Repo + "@" + s.Source.Ref
		sha, ok := resolved[key]
		if !ok {
			sha, err = client.ResolveCommit(ctx, s.Source.Repo, s.Source.Ref)
			if err != nil {
				fmt.Printf("エラー: %s — %v\n", s.Name, err)
				failures++
				continue
			}
			resolved[key] = sha
		}

		if sha == s.Source.Commit {
			fmt.Printf("変更なし: %s(commit一致 %s)\n", s.Name, shortSHA(sha))
			continue
		}

		// commitが動いた(または未記入)。コンテンツハッシュで最終判定する
		files, err := client.FetchDir(ctx, s.Source.Repo, sha, s.Source.Subdir)
		if err != nil {
			fmt.Printf("エラー: %s — %v\n", s.Name, err)
			failures++
			continue
		}
		current := make(map[string]string, len(files))
		for p, b := range files {
			current[p] = lockfile.HashBytes(b)
		}
		changes := lockfile.DiffFiles(s.Files, current)
		if len(changes) == 0 {
			fmt.Printf("変更なし: %s(commitは %s → %s に進んだが内容は同一)\n", s.Name, shortSHA(s.Source.Commit), shortSHA(sha))
			continue
		}

		driftCount++
		fmt.Printf("\nドリフト: %s(%s", s.Name, s.Source.Repo)
		if s.Source.Subdir != "" {
			fmt.Printf(" の %s", s.Source.Subdir)
		}
		fmt.Printf(")\n")
		fmt.Printf("  commit: %s → %s\n", shortSHA(s.Source.Commit), shortSHA(sha))
		for _, c := range changes {
			fmt.Printf("  %s %s\n", changeLabel(c.Kind), c.Path)
		}
		fmt.Println()
	}

	if untracked > 0 {
		fmt.Printf("出自未記入のためスキップ: %d件(%s の source.repo を記入すると監視対象になる)\n", untracked, lockPath)
	}
	switch {
	case failures > 0:
		return driftCount > 0, fmt.Errorf("check: %d件のスキルで上流の取得に失敗した", failures)
	case driftCount > 0:
		fmt.Printf("ドリフトを%d件検知した。上流の変更内容を確認してください\n", driftCount)
		return true, nil
	default:
		fmt.Println("ドリフトなし")
		return false, nil
	}
}

func changeLabel(k lockfile.ChangeKind) string {
	switch k {
	case lockfile.ChangeAdded:
		return "追加:"
	case lockfile.ChangeRemoved:
		return "削除:"
	default:
		return "変更:"
	}
}

func shortSHA(sha string) string {
	if sha == "" {
		return "(未記入)"
	}
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
