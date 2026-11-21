package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"atoll/connection"
	"atoll/repo"
	"atoll/store"
)

const (
	_maxBackoff   = 15
	_noCheckEvery = 5
	_nameCut      = 60
	_namesListed  = 5
	_readTimeout  = 30 * time.Second
	_pauseDefault = time.Minute
	// _ciFailed follows the failed count in the wake of a CI run with a failed check.
	_ciFailed = " failed ("
)

var (
	_prNumber = regexp.MustCompile(`#\d+`)
)

type (
	// watcher is what the poll keeps in memory: waits and ETags per PR, rate-limit pauses per
	// connection.
	watcher struct {
		prs    map[string]*watched
		issues map[string]*watchedIssue
		paused map[string]time.Time
	}

	watched struct {
		etag  string
		pull  ghPull
		fails int
		next  time.Time

		noCheckPolls int
	}

	ghPull struct {
		State     string `json:"state"`
		Merged    bool   `json:"merged"`
		Draft     bool   `json:"draft"`
		Mergeable *bool  `json:"mergeable"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}

	// ciResult is the last read of a head commit's checks, as stored in the PR's ci.
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

// of returns the PR's entry.
func (w *watcher) of(p store.PR) *watched {
	key := p.ThreadID + " " + p.URL
	ws := w.prs[key]
	if ws == nil {
		ws = &watched{}
		w.prs[key] = ws
	}
	return ws
}

func (m *Manager) checkGitHub(w *watcher, wp store.WatchedPR, bot store.Bot, rp store.Repo) error {
	t, p := wp.Thread, wp.PR
	connID, token, err := m.watchToken(bot)
	if err != nil || token == "" {
		return err
	}
	if time.Now().Before(w.paused[connID]) {
		return nil
	}
	owner, name, n, err := parsePullURL(p.URL)
	if err != nil {
		return err
	}
	api := fmt.Sprintf("%s/repos/%s/%s", connection.GitHubAPI(), owner, name)
	ws := w.of(p)
	body, hdr, err := m.githubGet(w, t.BotID, connID, token, fmt.Sprintf("%s/pulls/%d", api, n), ws.etag)
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
	title := p.Title
	if title == "" {
		title = ws.pull.Title
	}
	r := store.MRRead{State: state, Title: title, CI: p.CI, Woke: p.Woke, Head: p.Head}
	if ws.pull.Head.SHA != "" {
		r.Head = ws.pull.Head.SHA
	}
	label := prLabel(p.URL)
	switch {
	case state == _mrOpened:
		if err := m.readCI(w, connID, token, api, wp, ws.pull.Head.SHA, label, &r); err != nil {
			return err
		}
	case state == _mrMerged && p.State != _mrMerged:
		m.mergedText(rp, label, ws.pull, &r)
	}
	note := ""
	if state == _mrMerged && p.State != _mrMerged {
		if note, err = m.memoryNote(t.RepoID, ws.pull.Body); err != nil {
			return err
		}
	}
	return m.update(wp, label, r, note)
}

// mergedText sets the wake of a merge: where it went and at which commit. A merge into a branch
// other than the default is a quiet line.
func (m *Manager) mergedText(rp store.Repo, label string, pull ghPull, r *store.MRRead) {
	base := cleanName(pull.Base.Ref)
	r.WakeText = fmt.Sprintf("%s merged into %s at %s", label, base, short(pull.Head.SHA))
	if def := repo.DefaultBranch(m.ctx, rp.Path); def != "" && pull.Base.Ref != def {
		r.Quiet = true
	}
}

func short(sha string) string { return sha[:min(len(sha), 7)] }

// readCI reads the checks of the head commit until its CI is reported, and sets in r the stored
// result and, when CI finished, the wake. A finished result counts once seen on two reads in a row.
// A head with no checks, or whose stored result has a failed check, is read again only every
// _noCheckEvery polls, so a check that appears or a re-run on the same commit is still seen.
func (m *Manager) readCI(w *watcher, connID, token, api string, wp store.WatchedPR, head, label string, r *store.MRRead) error {
	p := wp.PR
	var prev ciResult
	if p.CI != "" {
		_ = json.Unmarshal([]byte(p.CI), &prev)
	}
	if prev.Head != "" && prev.Head != head {
		r.DropCI = true
	}
	if head == "" {
		return nil
	}
	recheck := false
	if p.Woke == head {
		if prev.Head != head || !prev.none() && prev.Failed == 0 {
			return nil
		}
		ws := w.of(p)
		if ws.noCheckPolls++; ws.noCheckPolls%_noCheckEvery != 0 {
			return nil
		}
		recheck = true
	}
	checks, err := m.readChecks(w, wp.Thread.BotID, connID, token, api, head)
	if err != nil {
		return err
	}
	res := ciResult{Head: head}
	for _, c := range checks {
		res.add(c.Name, c.Result)
	}
	enc, err := json.Marshal(res)
	if err != nil {
		return err
	}
	r.CI = string(enc)
	if res.none() {
		if prev.Head == head && prev.none() {
			r.Woke = head
		}
		return nil
	}
	if recheck {
		if res.same(prev) {
			return nil
		}
		r.Woke = ""
	}
	finished := res.Passed+res.Failed > 0 && res.Pending == 0
	if !finished || p.Wake && (prev.Head != head || !res.same(prev)) {
		return nil
	}
	r.Woke = head
	if p.Wake {
		r.WakeKind, r.WakeText, r.Notice = "ci", ciText(label, res), ciNotice(res)
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

func (c ciResult) none() bool { return c.Passed+c.Failed+c.Pending == 0 }

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
func (m *Manager) update(wp store.WatchedPR, label string, r store.MRRead, note string) error {
	t, p := wp.Thread, wp.PR
	r.URL, r.Prev, r.Label = p.URL, p.State, label
	if label != "" && p.Wake && r.State != p.State && (r.State == _mrMerged || r.State == _mrClosed) {
		r.WakeKind, r.DropCI, r.Notice = r.State, true, r.State
		if r.WakeText == "" || r.State == _mrClosed {
			r.WakeText = label + " " + r.State
		}
		r.WakeText += note
	}
	if p.Wake && r.WakeKind == "" && r.State == p.State && r.Title == p.Title && r.CI == p.CI &&
		r.Woke == p.Woke && r.Head == p.Head && !r.DropCI {
		return nil
	}
	stored, err := m.cfg.Store.StoreMR(m.ctx, t.ID, r)
	if err != nil || !stored {
		return err
	}
	if r.State != p.State || r.Title != p.Title {
		runID, err := m.cfg.Store.LatestRun(m.ctx, t.ID)
		if err != nil {
			return err
		}
		m.announce(t.ID, runID, EventMRUpdated, MRUpdated{RunID: runID, URL: p.URL, State: r.State, Title: r.Title})
	}
	if r.WakeKind != "" && !r.Quiet {
		m.cfg.Log.Info("wake bot", "thread_id", t.ID, "kind", r.WakeKind, "text", r.WakeText)
		m.flushLog(t.ID)
	}
	return nil
}

// watchToken is the token of the Bot's first GitHub connection, from the secret store only: none
// when the Bot has no GitHub connection or it needs sign-in.
func (m *Manager) watchToken(bot store.Bot) (id, token string, err error) {
	c, ok := m.botGitHub(m.ctx, bot)
	if !ok || c.Status != store.ConnectionConnected {
		return "", "", nil
	}
	token, err = m.githubToken(m.ctx, c.ID, bot.ID, _githubRefreshAtExpiry)
	if err != nil {
		return "", "", err
	}
	return c.ID, token, nil
}

// githubGet reads one GitHub API URL. A 304 returns a nil body. A 401 marks the connection
// needs sign-in; a 403 or 429 that says the rate limit is spent pauses the connection's reads.
func (m *Manager) githubGet(w *watcher, botID, connID, token, rawURL, etag string) ([]byte, http.Header, error) {
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
		return nil, nil, m.githubSignInLost(m.ctx, connID, botID, connection.RejectedBy(rawURL, resp))
	case http.StatusForbidden, http.StatusTooManyRequests:
		h := resp.Header
		if h.Get("X-RateLimit-Remaining") == "0" || h.Get("Retry-After") != "" {
			w.paused[connID] = pauseUntil(h)
		}
	}
	return nil, nil, &githubStatusError{Status: resp.StatusCode, URL: rawURL}
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
	return fmt.Sprintf("%s, %d"+_ciFailed+"%s)", text, c.Failed, strings.Join(names, ", "))
}

func ciNotice(c ciResult) string {
	if c.Failed > 0 {
		return store.NoticeCIFailed
	}
	return store.NoticeCIPassed
}

func quoteName(s string) string { return `"` + cleanName(s) + `"` }

// cleanName drops control characters, quotes and backslashes of s and cuts it.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp) || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > _nameCut {
		s = string([]rune(s)[:_nameCut])
	}
	return s
}
