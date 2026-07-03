// skilldrift は導入済みClaude Code Skillsの上流変更を監視するCLI。
// lockfile(skilldrift.lock)に記録した出自と上流の現在状態を比較し、ドリフトを検知する。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kpab/skilldrift/internal/lockfile"
	"github.com/kpab/skilldrift/internal/scan"
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
		err = runCheck(os.Args[2:])
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
		os.Exit(1)
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

func runCheck(args []string) error {
	return fmt.Errorf("check: 未実装(M1)")
}
