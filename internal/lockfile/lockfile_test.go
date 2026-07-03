package lockfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sampleLockfile() *Lockfile {
	return &Lockfile{
		Version: 1,
		Skills: []Skill{
			{
				Name: "deep-research",
				Path: "skills/deep-research",
				Source: Source{
					Type:   SourceTypeGitHub,
					Repo:   "owner/skills-repo",
					Ref:    "main",
					Subdir: "skills/deep-research",
					Commit: "0123abcd",
				},
				Files: map[string]string{
					"SKILL.md":         HashBytes([]byte("# deep-research\n")),
					"scripts/fetch.sh": HashBytes([]byte("#!/bin/sh\n")),
				},
			},
			{
				Name:   "local-only",
				Path:   "skills/local-only",
				Source: Source{Type: SourceTypeGitHub},
				Files:  map[string]string{"SKILL.md": HashBytes([]byte("hi"))},
			},
		},
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	want := sampleLockfile()
	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ラウンドトリップで内容が変わった:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestSaveDeterministic(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.lock")
	p2 := filepath.Join(dir, "b.lock")

	lf := sampleLockfile()
	// name順でない並びでもソートされて同一出力になること
	lf.Skills[0], lf.Skills[1] = lf.Skills[1], lf.Skills[0]
	if err := lf.Save(p1); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := sampleLockfile().Save(p2); err != nil {
		t.Fatalf("Save: %v", err)
	}

	b1, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(p2)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Errorf("並び順の違いで出力が変わった:\n%s\n---\n%s", b1, b2)
	}
	if !strings.HasSuffix(string(b1), "}\n") {
		t.Errorf("末尾改行がない: %q", string(b1)[len(b1)-5:])
	}
	loaded, err := Load(p1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Skills[0].Name != "deep-research" {
		t.Errorf("name昇順で保存されていない: 先頭が %q", loaded.Skills[0].Name)
	}
}

// スキーマ(シリアライズ形式)を固定するgoldenテスト。
// このテストを壊す変更は後方互換性の検討が必要。
func TestSaveGolden(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	lf := &Lockfile{
		Version: 1,
		Skills: []Skill{
			{
				Name: "example",
				Path: "skills/example",
				Source: Source{
					Type:   SourceTypeGitHub,
					Repo:   "owner/repo",
					Ref:    "main",
					Subdir: "skills/example",
					Commit: "abc123",
				},
				Files: map[string]string{"SKILL.md": "sha256:deadbeef"},
			},
		},
	}
	if err := lf.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 1,
  "skills": [
    {
      "name": "example",
      "path": "skills/example",
      "source": {
        "type": "github",
        "repo": "owner/repo",
        "ref": "main",
        "subdir": "skills/example",
        "commit": "abc123"
      },
      "files": {
        "SKILL.md": "sha256:deadbeef"
      }
    }
  ]
}
`
	if string(got) != want {
		t.Errorf("シリアライズ形式が変わった:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestLoadNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.lock"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("fs.ErrNotExist を期待したが: %v", err)
	}
}

func TestLoadVersionTooNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	if err := os.WriteFile(path, []byte(`{"version": 999, "skills": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "999") {
		t.Errorf("新しすぎるversionのエラーを期待したが: %v", err)
	}
}

func TestValidate(t *testing.T) {
	dup := sampleLockfile()
	dup.Skills[1].Name = dup.Skills[0].Name
	noName := sampleLockfile()
	noName.Skills[0].Name = ""
	noPath := sampleLockfile()
	noPath.Skills[0].Path = ""

	for _, tt := range []struct {
		label string
		lf    *Lockfile
		want  string
	}{
		{"名前重複", dup, "重複"},
		{"name空", noName, "name が空"},
		{"path空", noPath, "path が空"},
	} {
		err := tt.lf.Save(filepath.Join(t.TempDir(), DefaultPath))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %q を含むエラーを期待したが: %v", tt.label, tt.want, err)
		}
	}
}

func TestSaveFillsVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	lf := &Lockfile{} // version未設定
	if err := lf.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != SchemaVersion {
		t.Errorf("version = %d, want %d", got.Version, SchemaVersion)
	}
}

func TestTracked(t *testing.T) {
	lf := sampleLockfile()
	if !lf.Skills[0].Tracked() {
		t.Error("repo記入済みのスキルが Tracked() = false")
	}
	if lf.Skills[1].Tracked() {
		t.Error("repo未記入のスキルが Tracked() = true")
	}
}

func TestReconcile(t *testing.T) {
	lf := sampleLockfile() // deep-research(出自記入済み)と local-only
	found := []Skill{
		{
			Name:   "deep-research",
			Path:   "skills/deep-research",
			Source: Source{Type: SourceTypeGitHub}, // 走査結果は常に未記入
			Files:  map[string]string{"SKILL.md": "sha256:updated"},
		},
		{
			Name:   "newcomer",
			Path:   "skills/newcomer",
			Source: Source{Type: SourceTypeGitHub},
			Files:  map[string]string{"SKILL.md": "sha256:new"},
		},
	}

	ch := lf.Reconcile(found)

	if !reflect.DeepEqual(ch.Added, []string{"newcomer"}) {
		t.Errorf("Added = %v", ch.Added)
	}
	if !reflect.DeepEqual(ch.Removed, []string{"local-only"}) {
		t.Errorf("Removed = %v", ch.Removed)
	}
	if len(lf.Skills) != 2 {
		t.Fatalf("Skills = %d件, 2件を期待", len(lf.Skills))
	}
	byName := map[string]Skill{}
	for _, s := range lf.Skills {
		byName[s.Name] = s
	}
	dr := byName["deep-research"]
	if dr.Source.Repo != "owner/skills-repo" || dr.Source.Commit != "0123abcd" {
		t.Errorf("既存エントリのSourceが保持されていない: %+v", dr.Source)
	}
	if dr.Files["SKILL.md"] != "sha256:updated" {
		t.Errorf("Filesが走査結果で上書きされていない: %v", dr.Files)
	}
	if byName["newcomer"].Tracked() {
		t.Error("新規エントリが出自記入済みになっている")
	}
}

func TestDiffFiles(t *testing.T) {
	locked := map[string]string{
		"SKILL.md":     "sha256:aaa",
		"scripts/x.sh": "sha256:bbb",
		"gone.txt":     "sha256:ccc",
	}
	current := map[string]string{
		"SKILL.md":     "sha256:aaa", // 同一
		"scripts/x.sh": "sha256:XXX", // 変更
		"new.py":       "sha256:ddd", // 追加
	}
	got := DiffFiles(locked, current)
	want := []FileChange{
		{Path: "gone.txt", Kind: ChangeRemoved},
		{Path: "new.py", Kind: ChangeAdded},
		{Path: "scripts/x.sh", Kind: ChangeModified},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffFiles = %+v, want %+v", got, want)
	}

	if diff := DiffFiles(locked, locked); len(diff) != 0 {
		t.Errorf("同一マップでdiffが出た: %+v", diff)
	}
}

func TestHashBytes(t *testing.T) {
	// echo -n hello | shasum -a 256
	want := "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got := HashBytes([]byte("hello")); got != want {
		t.Errorf("HashBytes = %q, want %q", got, want)
	}
}

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := HashBytes([]byte("hello")); got != want {
		t.Errorf("HashFile = %q, want %q", got, want)
	}
	if _, err := HashFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("存在しないファイルでエラーにならない")
	}
}
