package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestListOpenIssuesFiltersPullRequests(t *testing.T) {
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/issues" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state = %q", got)
		}
		fmt.Fprint(w, `[
			{"number": 1, "title": "issue", "body": "hello"},
			{"number": 2, "title": "pr", "body": "pr body", "pull_request": {"url": "x"}}
		]`)
	}))
	defer done()

	got, err := c.ListOpenIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListOpenIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("PRを除外した1件を期待したが: %+v", got)
	}
}

func TestListOpenIssuesPagination(t *testing.T) {
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		w.Write([]byte("["))
		switch page {
		case "1":
			for i := 1; i <= 100; i++ {
				if i > 1 {
					w.Write([]byte(","))
				}
				fmt.Fprintf(w, `{"number": %d, "title": "t", "body": ""}`, i)
			}
		case "2":
			fmt.Fprint(w, `{"number": 101, "title": "t", "body": ""}`)
		default:
			t.Errorf("予期しないpage = %q", page)
		}
		w.Write([]byte("]"))
	}))
	defer done()

	got, err := c.ListOpenIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListOpenIssues: %v", err)
	}
	if len(got) != 101 {
		t.Errorf("2ページ分の101件を期待したが %d件", len(got))
	}
}

func TestCreateIssue(t *testing.T) {
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/owner/repo/issues" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			Title  string   `json:"title"`
			Body   string   `json:"body"`
			Labels []string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Title != "タイトル" || payload.Body != "本文" || len(payload.Labels) != 1 {
			t.Errorf("payload = %+v", payload)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"number": 42, "title": "タイトル", "body": "本文"}`)
	}))
	defer done()

	is, err := c.CreateIssue(context.Background(), "owner/repo", "タイトル", "本文", []string{"skilldrift"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if is.Number != 42 {
		t.Errorf("Number = %d, want 42", is.Number)
	}
}

func TestUpdateIssueAndComment(t *testing.T) {
	var gotPatch, gotComment bool
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo/issues/7":
			gotPatch = true
			fmt.Fprint(w, `{"number": 7}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/comments":
			gotComment = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id": 1}`)
		default:
			t.Errorf("予期しないリクエスト: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer done()

	if err := c.UpdateIssue(context.Background(), "owner/repo", 7, "t", "b"); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if err := c.CommentIssue(context.Background(), "owner/repo", 7, "c"); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	if !gotPatch || !gotComment {
		t.Errorf("patch=%v comment=%v", gotPatch, gotComment)
	}
}

func TestCreateIssueForbidden(t *testing.T) {
	c, done := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Resource not accessible"}`, http.StatusForbidden)
	}))
	defer done()

	_, err := c.CreateIssue(context.Background(), "owner/repo", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "issues: write") {
		t.Errorf("権限不足を示すエラーを期待したが: %v", err)
	}
}
