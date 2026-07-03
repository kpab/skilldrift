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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Client はGitHub APIクライアント。ゼロ値では使わず New で作る。
type Client struct {
	HTTP    *http.Client
	BaseURL string // テスト用に差し替え可能。既定は api.github.com
	Token   string
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
func (c *Client) FetchDir(ctx context.Context, repo, commit, subdir string) (map[string][]byte, error) {
	if err := validateRepo(repo); err != nil {
		return nil, err
	}
	body, err := c.get(ctx, fmt.Sprintf("/repos/%s/tarball/%s", repo, url.PathEscape(commit)), "")
	if err != nil {
		return nil, err
	}

	prefix := path.Clean(subdir)
	if prefix == "." || prefix == "/" {
		prefix = ""
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
		if prefix != "" {
			if !strings.HasPrefix(rest, prefix+"/") {
				continue
			}
			rest = strings.TrimPrefix(rest, prefix+"/")
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("%s の %s の読み込みに失敗: %w", repo, hdr.Name, err)
		}
		files[rest] = data
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s@%s: %q にファイルが無い(source.subdirの記載を確認)", repo, shortSHA(commit), subdir)
	}
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

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("GitHub: %s が見つからない(repo/ref/commitの記載、privateリポジトリならGITHUB_TOKENを確認)", apiPath)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, fmt.Errorf("GitHub: %s へのアクセスが拒否された(rate limitの可能性。GITHUB_TOKENの設定で緩和できる): HTTP %d", apiPath, resp.StatusCode)
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
