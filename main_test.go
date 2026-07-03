package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kpab/skilldrift/internal/lockfile"
)

func TestRunInit(t *testing.T) {
	dir := t.TempDir()
	skillMd := filepath.Join(dir, "skills", "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillMd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMd, []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit([]string{"-dir", dir}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	lockPath := filepath.Join(dir, lockfile.DefaultPath)
	lf, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatalf("生成されたlockfileが読めない: %v", err)
	}
	if len(lf.Skills) != 1 || lf.Skills[0].Name != "alpha" {
		t.Fatalf("Skills = %+v", lf.Skills)
	}
	if lf.Skills[0].Tracked() {
		t.Error("init直後は出自未記入のはず")
	}

	// 出自を手で記入して再実行 → 保持されること
	lf.Skills[0].Source.Repo = "owner/upstream"
	lf.Skills[0].Source.Commit = "abc123"
	if err := lf.Save(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := runInit([]string{"-dir", dir}); err != nil {
		t.Fatalf("runInit(再実行): %v", err)
	}
	lf, err = lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lf.Skills[0].Source.Repo != "owner/upstream" {
		t.Errorf("再実行で手記入のsourceが失われた: %+v", lf.Skills[0].Source)
	}
}

func TestRunInitNoSkills(t *testing.T) {
	if err := runInit([]string{"-dir", t.TempDir()}); err == nil {
		t.Error("スキルなしディレクトリでエラーにならない")
	}
}
