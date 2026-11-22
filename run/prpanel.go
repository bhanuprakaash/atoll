package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"atoll/connection"
	"atoll/store"
)

type (
	// PullRequest is a GitHub pull request as the PR panel shows it.
	PullRequest struct {
		URL         string   `json:"url"`
		Repo        string   `json:"repo"`
		Number      int      `json:"number"`
		Title       string   `json:"title"`
		Author      string   `json:"author"`
		Base        string   `json:"base"`
		Head        string   `json:"head"`
		HeadSHA     string   `json:"head_sha"`
		State       string   `json:"state"`
		Body        string   `json:"body"`
		Checks      []Check  `json:"checks"`
		Files       []PRFile `json:"files"`
		MergeMethod string   `json:"merge_method"`
		Mergeable   *bool    `json:"mergeable"`
	}

	// Check is one check run or commit status: passed, failed or pending.
	Check struct {
		Name   string `json:"name"`
		Result string `json:"result"`
	}

	// PRFile is one changed file. Patch is empty when GitHub sends none (binary, too large).
	PRFile struct {
		Path      string `json:"path"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Patch     string `json:"patch,omitempty"`
	}

	// PRError is a PR read or merge Reef could not do. Code names the cause for the UI; Text is the
	// sentence for the person.
	PRError struct {
		Code string
		Text string
		Err  error
	}

	ghRepo struct {
		AllowMergeCommit *bool `json:"allow_merge_commit"`
		AllowSquashMerge *bool `json:"allow_squash_merge"`
		AllowRebaseMerge *bool `json:"allow_rebase_merge"`
	}

	// githubStatusError is a GitHub answer other than 200, 304 or 401.
	githubStatusError struct {
		Status int
		URL    string
	}
)

// Codes of PRError.
const (
	PRNoGitHub  = "no_github"
	PRSignIn    = "github_sign_in"
	PRCantSee   = "cant_see"
	PRGitHub    = "github"
	PRHeadMoved = "head_moved"
	PRRefused   = "refused"
)

const _messageCut = 200

// ErrNotPull is returned for a URL that is not a pull request on GitHub's host.
var ErrNotPull = errors.New("not a GitHub pull request URL")

func (e *PRError) Error() string {
	if e.Err != nil {
		return e.Text + ": " + e.Err.Error()
	}
	return e.Text
}

func (e *PRError) Unwrap() error { return e.Err }

func (e *githubStatusError) Error() string {
	return fmt.Sprintf("GitHub HTTP status %d for %s", e.Status, e.URL)
}

// ReadPR reads the pull request at rawURL for the PR panel with the thread Bot's first GitHub
// connection, the PR poll's rule: never unauthenticated, never the server's own token.
func (m *Manager) ReadPR(ctx context.Context, threadID, rawURL string) (PullRequest, error) {
	owner, name, n, err := pullOnGitHub(rawURL)
	if err != nil {
		return PullRequest{}, err
	}
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err != nil {
		return PullRequest{}, err
	}
	bot, err := m.cfg.Store.Bot(ctx, t.BotID)
	if err != nil {
		return PullRequest{}, err
	}
	connID, token, err := m.panelToken(ctx, bot, n)
	if err != nil {
		return PullRequest{}, err
	}
	w := &watcher{paused: map[string]time.Time{}}
	get := func(u string) ([]byte, http.Header, error) {
		return m.githubGet(w, bot.ID, connID, token, u, "")
	}
	api := fmt.Sprintf("%s/repos/%s/%s", connection.GitHubAPI(), owner, name)
	body, _, err := get(fmt.Sprintf("%s/pulls/%d", api, n))
	if err != nil {
		return PullRequest{}, panelError(bot, n, err)
	}
	var pull ghPull
	if err := json.Unmarshal(body, &pull); err != nil {
		return PullRequest{}, panelError(bot, n, fmt.Errorf("decode pull request: %w", err))
	}
	pr := PullRequest{
		URL: rawURL, Repo: owner + "/" + name, Number: n, Title: pull.Title, Author: pull.User.Login,
		Base: pull.Base.Ref, Head: pull.Head.Ref, HeadSHA: pull.Head.SHA, State: pullState(pull),
		Body: pull.Body, Checks: []Check{}, Files: []PRFile{}, Mergeable: pull.Mergeable,
	}
	checks, err := m.readChecks(w, bot.ID, connID, token, api, pull.Head.SHA)
	if err != nil {
		return PullRequest{}, panelError(bot, n, err)
	}
	pr.Checks = append(pr.Checks, checks...)
	slices.SortStableFunc(pr.Checks, func(a, b Check) int { return _resultOrder[a.Result] - _resultOrder[b.Result] })
	for next := fmt.Sprintf("%s/pulls/%d/files?per_page=100", api, n); next != ""; {
		body, hdr, err := get(next)
		if err != nil {
			return PullRequest{}, panelError(bot, n, err)
		}
		var page []struct {
			Filename  string `json:"filename"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
			Patch     string `json:"patch"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return PullRequest{}, panelError(bot, n, fmt.Errorf("decode pull request files: %w", err))
		}
		for _, f := range page {
			pr.Files = append(pr.Files, PRFile{Path: f.Filename, Additions: f.Additions, Deletions: f.Deletions, Patch: f.Patch})
		}
		next = nextLink(hdr.Get("Link"))
	}
	if body, _, err = get(api); err != nil {
		return PullRequest{}, panelError(bot, n, err)
	}
	var r ghRepo
	if err := json.Unmarshal(body, &r); err != nil {
		return PullRequest{}, panelError(bot, n, fmt.Errorf("decode repo: %w", err))
	}
	pr.MergeMethod = r.mergeMethod()
	return pr, nil
}

var _resultOrder = map[string]int{"failed": 0, "pending": 1, "passed": 2}

// panelToken is the token of the Bot's first GitHub connection, or a PRError saying why there is
// none.
func (m *Manager) panelToken(ctx context.Context, bot store.Bot, n int) (id, token string, err error) {
	c, ok := m.botGitHub(ctx, bot)
	if !ok {
		return "", "", &PRError{Code: PRNoGitHub, Text: bot.Name + " has no GitHub connection."}
	}
	if c.Status != store.ConnectionConnected {
		return "", "", panelError(bot, n, connection.ErrGitHubNeedsSignIn)
	}
	token, err = m.githubToken(ctx, c.ID, bot.ID, _githubRefreshAtExpiry)
	if err != nil {
		return "", "", panelError(bot, n, err)
	}
	return c.ID, token, nil
}

// botGitHub is the Bot's first GitHub connection.
func (m *Manager) botGitHub(ctx context.Context, bot store.Bot) (store.Connection, bool) {
	for _, id := range bot.Connections {
		c, err := m.cfg.Store.Connection(ctx, id)
		if err == nil && c.Kind == store.KindGitHub {
			return c, true
		}
	}
	return store.Connection{}, false
}

// panelError names a failed GitHub call for the person.
func panelError(bot store.Bot, n int, err error) error {
	var status *githubStatusError
	switch {
	case errors.Is(err, connection.ErrGitHubNeedsSignIn):
		return &PRError{Code: PRSignIn, Text: bot.Name + "'s GitHub connection needs sign-in.", Err: err}
	case errors.As(err, &status) && (status.Status == http.StatusNotFound || status.Status == http.StatusForbidden):
		return &PRError{Code: PRCantSee, Text: fmt.Sprintf("%s's GitHub connection can't see PR #%d.", bot.Name, n), Err: err}
	}
	return &PRError{Code: PRGitHub, Text: fmt.Sprintf("Couldn't load PR #%d.", n), Err: err}
}

// readChecks reads every check run and commit status of a commit.
func (m *Manager) readChecks(w *watcher, botID, connID, token, api, sha string) ([]Check, error) {
	var checks []Check
	for _, route := range []string{"check-runs", "status"} {
		next := fmt.Sprintf("%s/commits/%s/%s?per_page=100", api, sha, route)
		for next != "" {
			body, hdr, err := m.githubGet(w, botID, connID, token, next, "")
			if err != nil {
				return nil, err
			}
			var page ghChecks
			if err := json.Unmarshal(body, &page); err != nil {
				return nil, fmt.Errorf("decode checks: %w", err)
			}
			for _, c := range page.CheckRuns {
				checks = append(checks, Check{Name: c.Name, Result: runOutcome(c.Status, c.Conclusion)})
			}
			for _, c := range page.Statuses {
				checks = append(checks, Check{Name: c.Context, Result: statusOutcome(c.State)})
			}
			next = nextLink(hdr.Get("Link"))
		}
	}
	return checks, nil
}

// mergeMethod is the first method the repo allows: merge commit, squash, rebase. GitHub leaves a
// setting out for a token that can't push; a missing one counts as allowed, GitHub's default.
func (r ghRepo) mergeMethod() string {
	for _, m := range []struct {
		allow *bool
		name  string
	}{{r.AllowMergeCommit, "merge"}, {r.AllowSquashMerge, "squash"}, {r.AllowRebaseMerge, "rebase"}} {
		if m.allow == nil || *m.allow {
			return m.name
		}
	}
	return ""
}

func pullState(p ghPull) string {
	switch {
	case p.State == "closed" && p.Merged:
		return _mrMerged
	case p.State == "closed":
		return _mrClosed
	case p.Draft:
		return "draft"
	}
	return "open"
}

// pullOnGitHub splits a pull request URL on GitHub's host.
func pullOnGitHub(raw string) (owner, name string, n int, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", 0, ErrNotPull
	}
	base, err := url.Parse(connection.GitHubURL())
	if err != nil || u.Host != base.Host {
		return "", "", 0, ErrNotPull
	}
	owner, name, n, err = parsePullURL(raw)
	if err != nil {
		return "", "", 0, ErrNotPull
	}
	return owner, name, n, nil
}

// MergePR merges the pull request at rawURL with the thread Bot's first GitHub connection, only at
// sha, the head the panel showed, and with the first method the repo allows. It then runs the
// thread's check of that PR at once, so the merge shows and wakes as one merged on GitHub.
func (m *Manager) MergePR(ctx context.Context, threadID, rawURL, sha string) error {
	owner, name, n, err := pullOnGitHub(rawURL)
	if err != nil {
		return err
	}
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err != nil {
		return err
	}
	bot, err := m.cfg.Store.Bot(ctx, t.BotID)
	if err != nil {
		return err
	}
	connID, token, err := m.panelToken(ctx, bot, n)
	if err != nil {
		return err
	}
	w := &watcher{prs: map[string]*watched{}, paused: map[string]time.Time{}}
	api := fmt.Sprintf("%s/repos/%s/%s", connection.GitHubAPI(), owner, name)
	body, _, err := m.githubGet(w, bot.ID, connID, token, api, "")
	if err != nil {
		return panelError(bot, n, err)
	}
	var r ghRepo
	if err := json.Unmarshal(body, &r); err != nil {
		return panelError(bot, n, fmt.Errorf("decode repo: %w", err))
	}
	if err := m.githubMerge(ctx, bot, connID, token, fmt.Sprintf("%s/pulls/%d/merge", api, n), n, sha, r.mergeMethod()); err != nil {
		return err
	}
	prs, err := m.cfg.Store.Watched(ctx)
	if err != nil {
		m.cfg.Log.Warn("list watched PRs after merge", "error", err)
		return nil
	}
	for _, wp := range prs {
		if wp.Thread.ID != threadID || wp.PR.URL != rawURL {
			continue
		}
		if err := m.check(w, wp); err != nil {
			m.cfg.Log.Warn("check MR after merge", "thread_id", threadID, "url", rawURL, "error", err)
		}
		return nil
	}
	return nil
}

// githubMerge sends the merge. GitHub's 409 is a head that moved since sha; another refusal
// carries GitHub's own message.
func (m *Manager) githubMerge(ctx context.Context, bot store.Bot, connID, token, rawURL string, n int, sha, method string) error {
	payload, err := json.Marshal(struct {
		SHA         string `json:"sha"`
		MergeMethod string `json:"merge_method"`
	}{SHA: sha, MergeMethod: method})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, _readTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return panelError(bot, n, fmt.Errorf("send GitHub request: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()
	var answer struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&answer)
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return panelError(bot, n, m.githubSignInLost(m.ctx, connID, bot.ID, connection.RejectedBy(rawURL, resp)))
	case http.StatusConflict:
		return &PRError{Code: PRHeadMoved, Text: "New commits were pushed since you opened this PR. Nothing was merged."}
	case http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
		if text := plainMessage(answer.Message); text != "" {
			return &PRError{Code: PRRefused, Text: text, Err: &githubStatusError{Status: resp.StatusCode, URL: rawURL}}
		}
	}
	return &PRError{Code: PRGitHub, Text: fmt.Sprintf("Couldn't merge PR #%d.", n), Err: &githubStatusError{Status: resp.StatusCode, URL: rawURL}}
}

// plainMessage is GitHub's refusal as one line: control characters dropped, cut.
func plainMessage(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
	if utf8.RuneCountInString(s) > _messageCut {
		s = string([]rune(s)[:_messageCut]) + "…"
	}
	return s
}
