package e2e

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBots_Delete(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	b := s.newBot(t, "Dory", "claude")
	threads := []thread{s.newThread(t, b), s.newThread(t, b)}
	var runs []runInfo
	for _, th := range threads {
		events := s.events(t, th)
		runs = append(runs, s.finished(t, events, s.send(t, th, _keepPrompt).ID))
	}

	s.do(t, "DELETE", "/api/bots/"+b.ID, nil, nil, http.StatusNoContent)

	var bots []bot
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	for _, listed := range bots {
		if listed.ID == b.ID {
			t.Fatalf("GET /api/bots still lists deleted Bot %q", b.ID)
		}
	}
	s.do(t, "GET", "/api/bots/"+b.ID+"/threads", nil, nil, http.StatusNotFound)
	for i, th := range threads {
		s.do(t, "GET", "/api/threads/"+th.ID, nil, nil, http.StatusNotFound)
		s.do(t, "GET", "/api/runs/"+runs[i].ID, nil, nil, http.StatusNotFound)
	}
	s.do(t, "DELETE", "/api/bots/not-a-bot", nil, nil, http.StatusNotFound)
}

func TestBots_DeleteRemovesWorktree(t *testing.T) {
	t.Parallel()
	f := newRepoFixture(t, "claude")
	b := f.bot(t, "claude", "all", nil)
	_, path, _, _ := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")
	if !strings.Contains(gitIn(t, f.repo.Path, "worktree", "list"), path) {
		t.Fatalf("git worktree list does not show %q before deletion", path)
	}

	f.s.do(t, "DELETE", "/api/bots/"+b.ID, nil, nil, http.StatusNoContent)

	if strings.Contains(gitIn(t, f.repo.Path, "worktree", "list"), path) {
		t.Fatalf("git worktree list still shows deleted Bot's worktree %q", path)
	}
}

func TestBots_DeleteKeepsMemory(t *testing.T) {
	t.Parallel()
	f := newRepoFixture(t, "claude")
	b := f.bot(t, "claude", "all", nil)
	th := f.thread(t, b)
	run, events := memoryRun(t, f, th, _saveMemory)
	savedEvent := repoEvent(t, events, "memory_saved")
	raw, err := json.Marshal(savedEvent.Data["item"])
	if err != nil {
		t.Fatal(err)
	}
	var saved memoryItem
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Body != "Use SQLite WAL for the journal." ||
		saved.Why != "Readers must not block journal writes." ||
		saved.Status != "accepted" || saved.SourceBotID != b.ID ||
		saved.SourceThreadID != th.ID || saved.SourceRunID != run.ID {
		t.Fatalf("saved memory: %+v", saved)
	}

	f.s.do(t, "DELETE", "/api/bots/"+b.ID, nil, nil, http.StatusNoContent)

	items := memoryList(t, f.s)
	if len(items) != 1 || items[0] != saved {
		t.Fatalf("GET /api/memory after Bot deletion: %+v; want saved memory %+v", items, saved)
	}
}

func TestBots_DeleteChief(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	before := s.bot(t, "chief")
	var reply struct {
		Error string `json:"error"`
	}
	s.do(t, "DELETE", "/api/bots/chief", nil, &reply, http.StatusConflict)
	if reply.Error == "" {
		t.Fatal("DELETE /api/bots/chief returned no error message")
	}
	after := s.bot(t, "chief")
	if after.ID != "chief" || after.Name != before.Name || after.Provider != before.Provider ||
		after.Look != before.Look || after.Model != before.Model ||
		after.State != before.State || after.Activity != before.Activity ||
		string(after.RepoAccess) != string(before.RepoAccess) ||
		!slices.Equal(after.Connections, before.Connections) {
		t.Fatalf("Chief changed after DELETE: before %+v, after %+v", before, after)
	}
}

func TestBots_DeleteStopsRunningRun(t *testing.T) {
	t.Parallel()
	for _, provider := range _providers {
		t.Run(provider, func(t *testing.T) {
			m, s := start(t, provider)
			b := s.newBot(t, "Busy", provider)
			th := s.newThread(t, b)
			events := s.events(t, th)
			run := s.send(t, th, _hangPrompt)
			until(t, events, run.ID, "tool_call")
			sessions := m.sessions(t, provider)
			pid := sessions[len(sessions)-1].pid

			s.do(t, "DELETE", "/api/bots/"+b.ID, nil, nil, http.StatusNoContent)

			eventually(t, 5*time.Second, "the deleted Bot's provider process exits", func() bool {
				return gone(pid)
			})
			s.do(t, "GET", "/api/runs/"+run.ID, nil, nil, http.StatusNotFound)
			s.do(t, "GET", "/api/bots/"+b.ID, nil, nil, http.StatusNotFound)
		})
	}
}
