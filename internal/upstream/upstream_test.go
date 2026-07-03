package upstream

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testClient(handler http.Handler) (*Client, func()) {
	srv := httptest.NewServer(handler)
	c := &Client{
		HTTP:    &http.Client{Timeout: 5 * time.Second},
		BaseURL: srv.URL,
		Token:   "test-token",
	}
	return c, srv.Close
}

// makeTarball はGitHubのtarball形式(先頭に "{owner}-{repo}-{sha}/" ディレクトリ、
// pax_global_headerあり)を模したgzip圧縮tarを作る。
func makeTarball(t *testing.T, topDir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{
		Name:       "pax_global_header",
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": "abc123"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: topDir + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     topDir + "/" + name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(content)),
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
	return buf.Bytes()
}

func TestResolveCommit(t *testing.T) {
	sha := strings.Repeat("a", 40)
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/commits/main" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github.sha" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(sha + "\n"))
	}))
	defer done()

	got, err := c.ResolveCommit(context.Background(), "owner/repo", "main")
	if err != nil {
		t.Fatalf("ResolveCommit: %v", err)
	}
	if got != sha {
		t.Errorf("sha = %q, want %q", got, sha)
	}
}

func TestResolveCommitDefaultBranch(t *testing.T) {
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/commits/HEAD" {
			t.Errorf("ref未指定はHEADになるはず: path = %q", r.URL.Path)
		}
		w.Write([]byte(strings.Repeat("b", 40)))
	}))
	defer done()

	if _, err := c.ResolveCommit(context.Background(), "owner/repo", ""); err != nil {
		t.Fatalf("ResolveCommit: %v", err)
	}
}

func TestResolveCommitNotFound(t *testing.T) {
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer done()

	_, err := c.ResolveCommit(context.Background(), "owner/repo", "gone")
	if err == nil || !strings.Contains(err.Error(), "見つからない") {
		t.Errorf("404の説明付きエラーを期待したが: %v", err)
	}
}

func TestResolveCommitBadRepo(t *testing.T) {
	c := &Client{}
	for _, repo := range []string{"", "noslash", "a/b/c", "owner/", "/name", "ow ner/repo"} {
		if _, err := c.ResolveCommit(context.Background(), repo, "main"); err == nil {
			t.Errorf("repo %q でエラーにならない", repo)
		}
	}
}

func TestFetchDir(t *testing.T) {
	tarball := makeTarball(t, "owner-repo-abc123", map[string]string{
		"README.md":                 "top-level",
		"skills/alpha/SKILL.md":     "# alpha",
		"skills/alpha/scripts/x.sh": "#!/bin/sh",
		"skills/beta/SKILL.md":      "# beta",
	})
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/tarball/abc123" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write(tarball)
	}))
	defer done()

	got, err := c.FetchDir(context.Background(), "owner/repo", "abc123", "skills/alpha")
	if err != nil {
		t.Fatalf("FetchDir: %v", err)
	}
	want := map[string][]byte{
		"SKILL.md":     []byte("# alpha"),
		"scripts/x.sh": []byte("#!/bin/sh"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", keys(got), keys(want))
	}
}

func TestFetchDirWholeRepo(t *testing.T) {
	tarball := makeTarball(t, "owner-repo-abc123", map[string]string{
		"SKILL.md":  "# single",
		"helper.py": "pass",
	})
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	}))
	defer done()

	got, err := c.FetchDir(context.Background(), "owner/repo", "abc123", "")
	if err != nil {
		t.Fatalf("FetchDir: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("subdir未指定は全ファイルを返すはず: %v", keys(got))
	}
}

func TestFetchDirSubdirMissing(t *testing.T) {
	tarball := makeTarball(t, "owner-repo-abc123", map[string]string{"README.md": "x"})
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	}))
	defer done()

	_, err := c.FetchDir(context.Background(), "owner/repo", "abc123", "skills/nope")
	if err == nil || !strings.Contains(err.Error(), "subdir") {
		t.Errorf("subdir不在の説明付きエラーを期待したが: %v", err)
	}
}

func keys(m map[string][]byte) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
