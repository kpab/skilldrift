// skilldrift は導入済みClaude Code Skillsの上流変更を監視するCLI。
// lockfile(skilldrift.lock)に記録した出自と上流の現在状態を比較し、ドリフトを検知する。
package main

import (
	"fmt"
	"os"
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
	return fmt.Errorf("init: 未実装(M1)")
}

func runCheck(args []string) error {
	return fmt.Errorf("check: 未実装(M1)")
}
