package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type prReadError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// prRead is GET /api/threads/{id}/pr?url=pull and the status it answered with, so a test reads a
// 200 and an error with one call.
func (f *prWakeFixture) prRead(t *testing.T, th thread, pull string, out any) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "GET", f.s.url+"/api/threads/"+th.ID+"/pr?url="+url.QueryEscape(pull), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("decode %s: %v", b, err)
		}
	}
	return resp.StatusCode, b
}

// The Bot's own GitHub connection reads the pull request: title, branches, checks failed first, files
// with counts and no patch for a file GitHub sent none for, and the merge method the repo allows.
func TestPRRead_ReadsThePullRequestWithTheBotsConnection(t *testing.T) {
	f := newPRWakeFixture(t, "claude")
	th := f.openPR(t, 1)
	pull := f.gh.URL + "/octo/hello/pull/1"
	sha := headSHA(1)
	f.gh.setPullBody(1, "Fixes the **answer**.")
	f.gh.setChecks(sha,
		[]fakeCheck{
			{"e2e-go", "completed", "success"},
			{"test-guard", "completed", "failure"},
			{"check", "completed", "failure"},
		},
		[]fakeStatus{{"deploy", "pending"}})
	f.gh.setPullFiles(1,
		fakeFile{"answer.txt", 1, 1, "@@ -1 +1 @@\n-41\n+42"},
		fakeFile{"docs/big.bin", 0, 0, ""},
		fakeFile{"notes.md", 3, 0, "@@ -0,0 +1,3 @@\n+a\n+b\n+c"})
	f.gh.setMergeMethods(false, true, true)

	var got struct {
		URL       string `json:"url"`
		Repo      string `json:"repo"`
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Author    string `json:"author"`
		Base      string `json:"base"`
		Head      string `json:"head"`
		HeadSHA   string `json:"head_sha"`
		State     string `json:"state"`
		Body      string `json:"body"`
		Mergeable bool   `json:"mergeable"`
		Method    string `json:"merge_method"`
		Checks    []struct {
			Name   string `json:"name"`
			Result string `json:"result"`
		} `json:"checks"`
		Files []map[string]any `json:"files"`
	}
	if status, body := f.prRead(t, th, pull, &got); status != http.StatusOK {
		t.Fatalf("GET pr: status %d: %s", status, body)
	}
	if got.URL != pull || got.Repo != "octo/hello" || got.Number != 1 || got.Title != _githubPRTitle ||
		got.Author != _fakePRAuthor || got.Base != "main" || got.Head != "fix/seeded-bug" || got.HeadSHA != sha ||
		got.State != "open" || got.Body != "Fixes the **answer**." || !got.Mergeable || got.Method != "squash" {
		t.Errorf("pull request: %+v", got)
	}
	var checks []string
	for _, c := range got.Checks {
		checks = append(checks, c.Name+" "+c.Result)
	}
	if want := []string{"test-guard failed", "check failed", "deploy pending", "e2e-go passed"}; !reflect.DeepEqual(checks, want) {
		t.Errorf("checks %q, want failed first, then pending, then passed: %q", checks, want)
	}
	want := []map[string]any{
		{"path": "answer.txt", "additions": 1.0, "deletions": 1.0, "patch": "@@ -1 +1 @@\n-41\n+42"},
		{"path": "docs/big.bin", "additions": 0.0, "deletions": 0.0},
		{"path": "notes.md", "additions": 3.0, "deletions": 0.0, "patch": "@@ -0,0 +1,3 @@\n+a\n+b\n+c"},
	}
	if !reflect.DeepEqual(got.Files, want) {
		t.Errorf("files %v, want every page followed, patch absent for the file with none: %v", got.Files, want)
	}
	if q := f.gh.fileQueries(1); len(q) != 3 || !strings.Contains(q[0], "per_page=100") {
		t.Errorf("file requests %q, want 3 pages, the first with per_page=100", q)
	}
}

// A Bot with no GitHub connection and a pull request its connection cannot see each answer with a
// code the panel words; a URL that is not a pull request of the GitHub host is invalid.
func TestPRRead_Errors(t *testing.T) {
	f := newPRWakeFixture(t, "claude")
	th := f.openPR(t, 1)
	pull := f.gh.URL + "/octo/hello/pull/1"
	other := f.s.newThread(t, f.bot(t, "claude", nil))

	for _, c := range []struct {
		name   string
		th     thread
		pull   string
		status int
		code   string
		text   string
	}{
		{"no connection", other, pull, http.StatusConflict, "no_github", "Octo has no GitHub connection."},
		{"cannot see", th, f.gh.URL + "/octo/hello/pull/99", http.StatusConflict, "cant_see", "Octo's GitHub connection can't see PR #99."},
		{"not a pull of the host", th, "https://github.com/octo/hello/pull/1", http.StatusBadRequest, "invalid", ""},
		{"not a pull", th, f.gh.URL + "/octo/hello/issues/1", http.StatusBadRequest, "invalid", ""},
		{"unknown thread", thread{ID: "no-such-thread"}, pull, http.StatusNotFound, "not_found", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			status, body := f.prRead(t, c.th, c.pull, nil)
			var e prReadError
			_ = json.Unmarshal(body, &e)
			if status != c.status || e.Code != c.code || (c.text != "" && e.Error != c.text) {
				t.Errorf("status %d %s, want %d code %q error %q", status, body, c.status, c.code, c.text)
			}
		})
	}
}
