package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"atoll/connection"
	"atoll/memory"
	"atoll/repo"
	"atoll/store"
)

const _idsListed = 5

// _closes finds the issues a PR body closes, as GitHub reads them.
var _closes = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+(#[1-9][0-9]*)\b`)

// watchedIssue is what the poll keeps of one ticket: its ETag and back-off.
type watchedIssue struct {
	etag   string
	closed bool // seen closed once; the items retire on the next read
	fails  int
	next   time.Time
}

// pollIssues reads each issue that has an accepted memory item, with its ETag, and retires the
// items of a closed one. It uses the first signed-in GitHub connection; a repo not on GitHub is
// not read.
func (m *Manager) pollIssues(w *watcher) {
	if m.cfg.Memory == nil {
		return
	}
	tickets, err := m.cfg.Memory.Tickets(m.ctx)
	if err != nil {
		m.cfg.Log.Warn("list memory tickets", "error", err)
		return
	}
	if len(tickets) == 0 {
		clear(w.issues)
		return
	}
	connID, token, err := m.firstGitHub()
	if err != nil {
		m.cfg.Log.Warn("read GitHub connection", "error", err)
		return
	}
	live := map[string]bool{}
	apis := map[string]string{}
	for _, t := range tickets {
		key := t.RepoID + t.Number
		live[key] = true
		wi := w.issues[key]
		if wi == nil {
			wi = &watchedIssue{}
			w.issues[key] = wi
		}
		if token == "" || m.ctx.Err() != nil || time.Now().Before(w.paused[connID]) || time.Now().Before(wi.next) {
			continue
		}
		api, ok := apis[t.RepoID]
		if !ok {
			api = m.issuesAPI(t.RepoID)
			apis[t.RepoID] = api
		}
		if api == "" {
			continue
		}
		if err := m.checkIssue(w, wi, connID, token, api, t); err != nil {
			if m.ctx.Err() != nil {
				return
			}
			wi.fails, wi.next = m.backoff(wi.fails)
			m.cfg.Log.Warn("check issue", "repo_id", t.RepoID, "ticket", t.Number, "error", err)
			continue
		}
		wi.fails, wi.next = 0, time.Time{}
	}
	for key := range w.issues {
		if !live[key] {
			delete(w.issues, key)
		}
	}
}

func (m *Manager) checkIssue(w *watcher, wi *watchedIssue, connID, token, api string, t memory.Ticket) error {
	body, hdr, err := m.githubGet(w, connID, token, api+"/issues/"+strings.TrimPrefix(t.Number, "#"), wi.etag)
	if err != nil {
		return err
	}
	if body != nil {
		var issue struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &issue); err != nil {
			return fmt.Errorf("decode issue: %w", err)
		}
		if issue.State == "closed" {
			if !wi.closed {
				wi.closed = true
				return nil
			}
			if err := m.cfg.Memory.RetireTicket(m.ctx, t); err != nil {
				return err
			}
		}
		wi.closed, wi.etag = false, hdr.Get("ETag")
	}
	return nil
}

// issuesAPI is the GitHub API base of the repo with id, empty when it is gone or not on GitHub.
func (m *Manager) issuesAPI(id string) string {
	rp, err := m.cfg.Store.Repo(m.ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ""
	}
	if err != nil {
		m.cfg.Log.Warn("read repo", "repo_id", id, "error", err)
		return ""
	}
	owner, name, ok := connection.GitHubRepo(repo.Origin(m.ctx, rp.Path))
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s/repos/%s/%s", connection.GitHubAPI(), owner, name)
}

// firstGitHub is the id and token of the first signed-in GitHub connection; empty when there is none.
func (m *Manager) firstGitHub() (id, token string, err error) {
	conns, err := m.cfg.Store.Connections(m.ctx)
	if err != nil {
		return "", "", err
	}
	for _, c := range conns {
		if c.Kind != store.KindGitHub || c.Status != store.ConnectionConnected {
			continue
		}
		token, err := connection.GitHubToken(c.ID)
		if err != nil {
			return "", "", fmt.Errorf("read GitHub connection token: %w", err)
		}
		return c.ID, token, nil
	}
	return "", "", nil
}

// memoryNote is the part of a merged PR's wake that names the memory items about the tickets its
// body closes, by id only: no Bot-written text joins a wake.
func (m *Manager) memoryNote(repoID, body string) (string, error) {
	if m.cfg.Memory == nil || repoID == "" {
		return "", nil
	}
	var note strings.Builder
	seen := map[string]bool{}
	for _, match := range _closes.FindAllStringSubmatch(body, -1) {
		number := match[1]
		if seen[number] {
			continue
		}
		seen[number] = true
		ids, err := m.cfg.Memory.TicketItems(m.ctx, memory.Ticket{RepoID: repoID, Number: number})
		if err != nil {
			return "", err
		}
		if len(ids) == 0 {
			continue
		}
		more := ""
		if len(ids) > _idsListed {
			more = fmt.Sprintf(", and %d more", len(ids)-_idsListed)
			ids = ids[:_idsListed]
		}
		if note.Len() == 0 {
			note.WriteString(".")
		}
		fmt.Fprintf(&note, " Memory about %s to replace or retire: %s%s.", number, strings.Join(ids, ", "), more)
	}
	return note.String(), nil
}
