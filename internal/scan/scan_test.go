package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpab/skilldrift/internal/lockfile"
)

// writeFile は dir/rel にファイルを作る(親ディレクトリも作成)。
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkills(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/alpha/SKILL.md", "# alpha")
	writeFile(t, root, "skills/alpha/scripts/run.sh", "#!/bin/sh")
	writeFile(t, root, "skills/alpha/nested/SKILL.md", "alphaの一部。別スキル扱いしない")
	writeFile(t, root, "skills/beta/SKILL.md", "# beta")
	writeFile(t, root, "README.md", "not a skill")
	writeFile(t, root, ".git/SKILL.md", "gitディレクトリは走査しない")
	writeFile(t, root, "docs/notes.md", "SKILL.mdなしディレクトリ")

	got, err := Skills(root)
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("スキル%d件検出、2件を期待: %+v", len(got), got)
	}

	byName := map[string]lockfile.Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	alpha, ok := byName["alpha"]
	if !ok {
		t.Fatal("alpha が検出されていない")
	}
	if alpha.Path != "skills/alpha" {
		t.Errorf("alpha.Path = %q", alpha.Path)
	}
	if alpha.Tracked() {
		t.Error("走査直後のスキルは出自未記入のはず")
	}
	wantFiles := []string{"SKILL.md", "scripts/run.sh", "nested/SKILL.md"}
	if len(alpha.Files) != len(wantFiles) {
		t.Errorf("alpha.Files = %v, キー%v を期待", alpha.Files, wantFiles)
	}
	for _, f := range wantFiles {
		h, ok := alpha.Files[f]
		if !ok {
			t.Errorf("alpha.Files に %q がない", f)
			continue
		}
		if !strings.HasPrefix(h, "sha256:") {
			t.Errorf("alpha.Files[%q] = %q, sha256: プレフィックスを期待", f, h)
		}
	}
	if _, ok := byName["beta"]; !ok {
		t.Error("beta が検出されていない")
	}
}

func TestSkillsRootIsSkill(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "SKILL.md", "# single")
	writeFile(t, root, "helper.py", "pass")

	got, err := Skills(root)
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("スキル%d件検出、1件を期待", len(got))
	}
	if got[0].Name != filepath.Base(root) {
		t.Errorf("Name = %q, ルートのディレクトリ名 %q を期待", got[0].Name, filepath.Base(root))
	}
	if got[0].Path != "." {
		t.Errorf("Path = %q, \".\" を期待", got[0].Path)
	}
	if len(got[0].Files) != 2 {
		t.Errorf("Files = %v, 2件を期待", got[0].Files)
	}
}

func TestSkillsDuplicateName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/foo/SKILL.md", "x")
	writeFile(t, root, "b/foo/SKILL.md", "y")

	_, err := Skills(root)
	if err == nil || !strings.Contains(err.Error(), "foo") {
		t.Errorf("名前重複エラーを期待したが: %v", err)
	}
}

func TestSkillsSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/a/SKILL.md", "x")
	target := filepath.Join(root, "skills/a/SKILL.md")
	if err := os.Symlink(target, filepath.Join(root, "skills/a/link.md")); err != nil {
		t.Skipf("symlink作成不可: %v", err)
	}

	got, err := Skills(root)
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if _, ok := got[0].Files["link.md"]; ok {
		t.Error("symlinkがFilesに含まれている")
	}
}
