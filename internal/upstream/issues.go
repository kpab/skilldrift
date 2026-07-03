package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Issue はGitHub Issueのうちskilldriftが使う最小限のフィールド。
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// ListOpenIssues は repo のopenなIssueを全件返す(Pull Requestは除く)。
func (c *Client) ListOpenIssues(ctx context.Context, repo string) ([]Issue, error) {
	if err := validateRepo(repo); err != nil {
		return nil, err
	}
	const perPage = 100
	var all []Issue
	for page := 1; ; page++ {
		body, err := c.get(ctx, fmt.Sprintf("/repos/%s/issues?state=open&per_page=%d&page=%d", repo, perPage, page), "")
		if err != nil {
			return nil, err
		}
		// Issues APIはPRも返すため pull_request フィールドの有無で除外する
		var items []struct {
			Issue
			PullRequest *struct{} `json:"pull_request"`
		}
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("%s のIssue一覧の解析に失敗: %w", repo, err)
		}
		for _, it := range items {
			if it.PullRequest == nil {
				all = append(all, it.Issue)
			}
		}
		if len(items) < perPage {
			return all, nil
		}
	}
}

// CreateIssue は repo にIssueを作成して返す。
func (c *Client) CreateIssue(ctx context.Context, repo, title, body string, labels []string) (Issue, error) {
	if err := validateRepo(repo); err != nil {
		return Issue{}, err
	}
	payload := map[string]any{"title": title, "body": body}
	if len(labels) > 0 {
		payload["labels"] = labels
	}
	resp, err := c.send(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues", repo), payload)
	if err != nil {
		return Issue{}, err
	}
	var is Issue
	if err := json.Unmarshal(resp, &is); err != nil {
		return Issue{}, fmt.Errorf("Issue作成応答の解析に失敗: %w", err)
	}
	return is, nil
}

// UpdateIssue は既存Issueのタイトルと本文を書き換える。
func (c *Client) UpdateIssue(ctx context.Context, repo string, number int, title, body string) error {
	if err := validateRepo(repo); err != nil {
		return err
	}
	_, err := c.send(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/issues/%d", repo, number),
		map[string]any{"title": title, "body": body})
	return err
}

// CommentIssue は既存Issueにコメントを追加する。
func (c *Client) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	if err := validateRepo(repo); err != nil {
		return err
	}
	_, err := c.send(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number),
		map[string]any{"body": body})
	return err
}
