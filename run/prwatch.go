package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"atoll/connection"
	"atoll/store"
)

const (
	_maxBackoff   = 15
	_nameCut      = 60
	_namesListed  = 5
	_readTimeout  = 30 * time.Second
	_pauseDefault = time.Minute
)

type (
	// watcher is what the poll keeps in memory: waits and ETags per thread, rate-limit pauses per
	// connection.
	watcher struct {
		threads map[string]*watched
		issues  map[string]*watchedIssue
		paused  map[string]time.Time
	}

	watched struct {
		url   string
		etag  string
		pull  ghPull
		fails int
		next  time.Time
	}

	ghPull struct {
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}

	// ciResult is the last read of a head commit's checks, as stored in the thread's mr_ci.
	ciResult struct {
		Head    string   `json:"head"`
		Passed  int      `json:"passed"`
		Failed  int      `json:"failed"`
		Pending int      `json:"pending"`
		Names   []string `json:"names"`
	}

	ghChecks struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
)

// of returns the thread's entry; a new URL starts a new one.
func (w *watcher) of(t store.Thread) *watched {
	ws := w.threads[t.ID]
	if ws == nil || ws.url != t.MRURL {
		ws = &watched{url: t.MRURL}
		w.threads[t.ID] = ws
	}
	return ws
}

func (m *Manager) checkGitHub(w *watcher, t store.Thread, bot store.Bot) error {
	connID, token, err := m.watchToken(bot)
	if err != nil || token == "" {
		return err
	}
	if time.Now().Before(w.paused[connID]) {
		return nil
	}
	owner, name, n, err := parsePullURL(t.MRURL)
	if err != nil {
		return err
	}
	api := fmt.Sprintf("%s/repos/%s/%s", connection.GitHubAPI(), owner, name)
	ws := w.of(t)
	body, hdr, err := m.githubGet(w, connID, token, fmt.Sprintf("%s/pulls/%d", api, n), ws.etag)
	if err != nil {
		return err
	}
	if body != nil {
		var pull ghPull
		if err := json.Unmarshal(body, &pull); err != nil {
			return fmt.Errorf("decode pull request: %w", err)
		}
		ws.pull, ws.etag = pull, hdr.Get("ETag")
	}
	state := _mrOpened
	switch {
	case ws.pull.State == "closed" && ws.pull.Merged:
		state = _mrMerged
	case ws.pull.State == "closed":
		state = _mrClosed
	}
	title := t.MRTitle
	if title == "" {
		title = ws.pull.Title
	}
	r := store.MRRead{State: state, Title: title, CI: t.MRCI, Woke: t.MRWoke}
	label := prLabel(t.MRURL)
	if state == _mrOpened {
		if err := m.readCI(w, connID, token, api, t, ws.pull.Head.SHA, label, &r); err != nil {
			return err
		}
	}
	note := ""
	if state == _mrMerged && t.MRState != _mrMerged {
		if note, err = m.memoryNote(t.RepoID, ws.pull.Body); err != nil {
			return err
		}
	}
	return m.update(t, label, r, note)
}

// readCI reads the checks of the head commit until its CI is reported, and sets in r the stored
// result and, when CI finished, the wake. A finished result counts once seen on two reads in a row.
func (m *Manager) readCI(w *watcher, connID, token, api string, t store.Thread, head, label string, r *store.MRRead) error {
	var prev ciResult
	if t.MRCI != "" {
		_ = json.Unmarshal([]byte(t.MRCI), &prev)
	}
	if prev.Head != "" && prev.Head != head {
		r.DropCI = true
	}
	if head == "" || t.MRWoke == head {
		return nil
	}
	res := ciResult{Head: head}
	for _, route := range []string{"check-runs", "status"} {
		next := fmt.Sprintf("%s/commits/%s/%s?per_page=100", api, head, route)
		for next != "" {
			body, hdr, err := m.githubGet(w, connID, token, next, "")
			if err != nil {
				return err
			}
			var page ghChecks
			if err := json.Unmarshal(body, &page); err != nil {
				return fmt.Errorf("decode checks: %w", err)
			}
			for _, c := range page.CheckRuns {
				res.add(c.Name, runOutcome(c.Status, c.Conclusion))
			}
			for _, c := range page.Statuses {
				res.add(c.Context, statusOutcome(c.State))
			}
			next = nextLink(hdr.Get("Link"))
		}
	}
	enc, err := json.Marshal(res)
	if err != nil {
		return err
	}
	r.CI = string(enc)
	finished := res.Passed+res.Failed > 0 && res.Pending == 0
	if !finished || t.MRWake && (prev.Head != head || !res.same(prev)) {
		return nil
	}
	r.Woke = head
	if t.MRWake {
		r.WakeKind, r.WakeText = "ci", ciText(label, res)
	}
	return nil
}

func (c *ciResult) add(name, outcome string) {
	switch outcome {
	case "passed":
		c.Passed++
	case "failed":
		c.Failed++
		if len(c.Names) < _namesListed {
			c.Names = append(c.Names, name)
		}
	default:
		c.Pending++
	}
}

func (c ciResult) same(o ciResult) bool {
	return c.Passed == o.Passed && c.Failed == o.Failed && c.Pending == o.Pending && slices.Equal(c.Names, o.Names)
}

func runOutcome(status, conclusion string) string {
	if status != "completed" {
		return "pending"
	}
	switch conclusion {
	case "success", "neutral", "skipped":
		return "passed"
	}
	return "failed"
}

func statusOutcome(state string) string {
	switch state {
	case "success":
		return "passed"
	case "failure", "error":
		return "failed"
	}
	return "pending"
}

// update stores what a poll read, with the wake for a new merged or closed state (note ends its
// text), then sends the change and delivers the wake. A GitLab MR has no label: it wakes nothing yet.
func (m *Manager) update(t store.Thread, label string, r store.MRRead, note string) error {
	r.URL, r.Prev = t.MRURL, t.MRState
	if label != "" && t.MRWake && r.State != t.MRState && (r.State == _mrMerged || r.State == _mrClosed) {
		r.WakeKind, r.WakeText, r.DropCI = r.State, label+" "+r.State+note, true
	}
	if t.MRWake && r.WakeKind == "" && r.State == t.MRState && r.Title == t.MRTitle && r.CI == t.MRCI &&
		r.Woke == t.MRWoke && !r.DropCI {
		return nil
	}
	stored, err := m.cfg.Store.StoreMR(m.ctx, t.ID, r)
	if err != nil || !stored {
		return err
	}
	if r.State != t.MRState || r.Title != t.MRTitle {
		runID, err := m.cfg.Store.LatestRun(m.ctx, t.ID)
		if err != nil {
			return err
		}
		m.announce(t.ID, runID, EventMRUpdated, MRUpdated{RunID: runID, URL: t.MRURL, State: r.State, Title: r.Title})
	}
	if r.WakeKind != "" {
		m.cfg.Log.Info("wake bot", "thread_id", t.ID, "kind", r.WakeKind, "text", r.WakeText)
		m.flushLog(t.ID)
	}
	return nil
}

// watchToken is the token of the Bot's first GitHub connection, from the secret store only: none
// when the Bot has no GitHub connection or it needs sign-in.
func (m *Manager) watchToken(bot store.Bot) (id, token string, err error) {
	for _, id := range bot.Connections {
		c, err := m.cfg.Store.Connection(m.ctx, id)
		if err != nil || c.Kind != store.KindGitHub {
			continue
		}
		if c.Status != store.ConnectionConnected {
			return "", "", nil
		}
		token, err := connection.GitHubToken(id)
		if err != nil {
			return "", "", fmt.Errorf("read GitHub connection token: %w", err)
		}
		return id, token, nil
	}
	return "", "", nil
}

// githubGet reads one GitHub API URL. A 304 returns a nil body. A 401 marks the connection
// needs sign-in; a 403 or 429 that says the rate limit is spent pauses the connection's reads.
func (m *Manager) githubGet(w *watcher, connID, token, rawURL, etag string) ([]byte, http.Header, error) {
	ctx, cancel := context.WithTimeout(m.ctx, _readTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("send GitHub request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		return body, resp.Header, err
	case http.StatusNotModified:
		return nil, resp.Header, nil
	case http.StatusUnauthorized:
		if err := m.cfg.Store.SetConnectionStatus(m.ctx, connID, store.ConnectionNeedsSignIn); err != nil {
			return nil, nil, fmt.Errorf("set GitHub connection needs sign-in: %w", err)
		}
		return nil, nil, connection.ErrGitHubNeedsSignIn
	case http.StatusForbidden, http.StatusTooManyRequests:
		h := resp.Header
		if h.Get("X-RateLimit-Remaining") == "0" || h.Get("Retry-After") != "" {
			w.paused[connID] = pauseUntil(h)
		}
	}
	return nil, nil, fmt.Errorf("GitHub HTTP status %d for %s", resp.StatusCode, rawURL)
}

func pauseUntil(h http.Header) time.Time {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil {
		return time.Now().Add(time.Duration(s) * time.Second)
	}
	if s, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		return time.Unix(s, 0)
	}
	return time.Now().Add(_pauseDefault)
}

// nextLink is the rel="next" URL of a Link header, or empty.
func nextLink(header string) string {
	for part := range strings.SplitSeq(header, ",") {
		target, params, _ := strings.Cut(part, ";")
		if strings.Contains(params, `rel="next"`) {
			return strings.Trim(strings.TrimSpace(target), "<>")
		}
	}
	return ""
}

// parsePullURL splits <host>/<owner>/<repo>/pull/<n>.
func parsePullURL(raw string) (owner, repo string, n int, err error) {
	u, perr := url.Parse(raw)
	if perr == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull" {
			if n, perr = strconv.Atoi(parts[3]); perr == nil {
				return parts[0], parts[1], n, nil
			}
		}
	}
	return "", "", 0, fmt.Errorf("not a GitHub pull request URL: %q", raw)
}

// prLabel is the PR as the thread names it: PR #7.
func prLabel(raw string) string {
	u, _ := url.Parse(raw)
	return "PR #" + path.Base(strings.TrimRight(u.Path, "/"))
}

// ciText is the CI wake: fixed words, numbers and check names. A name is data from anyone who can
// push, so it loses control characters, quotes and backslashes, is cut and quoted.
func ciText(label string, c ciResult) string {
	text := fmt.Sprintf("CI finished on %s: %d passed", label, c.Passed)
	if c.Failed == 0 {
		return text
	}
	names := make([]string, len(c.Names))
	for i, n := range c.Names {
		names[i] = quoteName(n)
	}
	if more := c.Failed - len(c.Names); more > 0 {
		names = append(names, fmt.Sprintf("and %d more", more))
	}
	return fmt.Sprintf("%s, %d failed (%s)", text, c.Failed, strings.Join(names, ", "))
}

func quoteName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp) || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > _nameCut {
		s = string([]rune(s)[:_nameCut])
	}
	return `"` + s + `"`
}
