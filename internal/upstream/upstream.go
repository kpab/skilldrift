// Package upstream はGitHub REST APIから上流スキルの現在状態
// (commit SHA・ファイル内容)を取得する。
//
// gh CLIには依存せず標準ライブラリのみでAPIを直接叩く(ローカル利用者に
// 導入前提を増やさないため)。認証は GITHUB_TOKEN / GH_TOKEN があれば使い、
// なければ匿名(rate limit 60回/時)。ファイル内容はtarball APIで
// リポジトリ×commitあたり1リクエストにまとめて取得する。
package upstream

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Client はGitHub APIクライアント。ゼロ値では使わず New で作る。
type Client struct {
	HTTP    *http.Client
	BaseURL string // テスト用に差し替え可能。既定は api.github.com
	Token   string

	mu    sync.Mutex
	trees map[string]map[string][]byte // "repo@commit" -> 展開済みtarball(全ファイル)
}

// New は環境変数 GITHUB_TOKEN(なければ GH_TOKEN)を読んでクライアントを作る。
func New() *Client {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return &Client{
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		BaseURL: defaultBaseURL,
		Token:   token,
	}
}

// ResolveCommit は repo("owner/name")の ref(ブランチ/タグ/SHA)が
// 現在指しているcommit SHAを返す。ref が空ならデフォルトブランチ(HEAD)。
func (c *Client) ResolveCommit(ctx context.Context, repo, ref string) (string, error) {
	if err := validateRepo(repo); err != nil {
		return "", err
	}
	if ref == "" {
		ref = "HEAD"
	}
	body, err := c.get(ctx, fmt.Sprintf("/repos/%s/commits/%s", repo, url.PathEscape(ref)), "application/vnd.github.sha")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(body))
	if len(sha) != 40 && len(sha) != 64 {
		return "", fmt.Errorf("%s@%s: commit SHAの応答が不正: %q", repo, ref, sha)
	}
	return sha, nil
}

// FetchDir は repo の commit 時点における subdir(空ならリポジトリ全体)配下の
// 全通常ファイルの内容を返す。キーは subdir からの相対パス(区切りは "/")で、
// lockfileのFilesと直接比較できる形。
// 同一repo×commitのtarballは1回だけ取得しキャッシュするため、
// 同じ上流の複数スキルを順に調べても再ダウンロードは発生しない。
func (c *Client) FetchDir(ctx context.Context, repo, commit, subdir string) (map[string][]byte, error) {
	if err := validateRepo(repo); err != nil {
		return nil, err
	}
	tree, err := c.fetchTree(ctx, repo, commit)
	if err != nil {
		return nil, err
	}

	prefix := path.Clean(subdir)
	if prefix == "." || prefix == "/" {
		prefix = ""
	}
	files := map[string][]byte{}
	for p, data := range tree {
		if prefix != "" {
			if !strings.HasPrefix(p, prefix+"/") {
				continue
			}
			p = strings.TrimPrefix(p, prefix+"/")
		}
		files[p] = data
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s@%s: %q にファイルが無い(source.subdirの記載を確認)", repo, shortSHA(commit), subdir)
	}
	return files, nil
}

// fetchTree は repo@commit のtarballを取得して全通常ファイルを展開し、キャッシュする。
func (c *Client) fetchTree(ctx context.Context, repo, commit string) (map[string][]byte, error) {
	key := repo + "@" + commit
	c.mu.Lock()
	tree, ok := c.trees[key]
	c.mu.Unlock()
	if ok {
		return tree, nil
	}

	body, err := c.get(ctx, fmt.Sprintf("/repos/%s/tarball/%s", repo, url.PathEscape(commit)), "")
	if err != nil {
		return nil, err
	}

	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s のtarballの展開に失敗: %w", repo, err)
	}
	defer gz.Close()

	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s のtarballの読み込みに失敗: %w", repo, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// tarballの先頭ディレクトリ("{owner}-{repo}-{sha}/")を落とす
		_, rest, ok := strings.Cut(hdr.Name, "/")
		if !ok || rest == "" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("%s の %s の読み込みに失敗: %w", repo, hdr.Name, err)
		}
		files[rest] = data
	}

	c.mu.Lock()
	if c.trees == nil {
		c.trees = map[string]map[string][]byte{}
	}
	c.trees[key] = files
	c.mu.Unlock()
	return files, nil
}

func (c *Client) get(ctx context.Context, apiPath, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+apiPath, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return c.do(req)
}

// send はJSONボディ付きのリクエスト(POST/PATCH)を送る。
func (c *Client) send(ctx context.Context, method, apiPath string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("GitHub APIリクエストのシリアライズに失敗: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+apiPath, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub APIへの接続に失敗: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("GitHub API応答の読み込みに失敗: %w", err)
	}

	apiPath := req.URL.Path
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("GitHub: %s が見つからない(repo/ref/commitの記載、privateリポジトリならGITHUB_TOKENを確認)", apiPath)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, fmt.Errorf("GitHub: %s へのアクセスが拒否された(rate limitか権限不足。書き込みには issues: write 権限のトークンが必要): HTTP %d", apiPath, resp.StatusCode)
	default:
		return nil, fmt.Errorf("GitHub: %s が HTTP %d を返した: %s", apiPath, resp.StatusCode, truncate(string(body), 200))
	}
}

func validateRepo(repo string) error {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || strings.ContainsAny(repo, " \t\n") {
		return fmt.Errorf("source.repo %q が不正(\"owner/name\" 形式が必要)", repo)
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
