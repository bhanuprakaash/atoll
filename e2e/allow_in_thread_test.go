package e2e

// Contract: allow-in-thread-contract.md.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	_cacheClean = "rm -rf /tmp/codex-gocache-161"
	_tmpClean   = "rm -rf /tmp/codex-gotmp-161"
)

// commandPrompt is the command-use prompt: the provider runs each command as its shell tool,
// asking first, and replies with one "<command>: ran" or "<command>: denied" line per command.
func commandPrompt(cmds ...string) string {
	b, _ := json.Marshal(cmds)
	return "Run commands. " + string(b)
}

// commandText is the text of an approval's command: Claude's `command`, or Codex's without its
// `bash -lc` wrapper, trimmed.
func commandText(e event) string {
	in, _ := e.Data["input"].(map[string]any)
	c := asString(in["command"])
	for _, p := range []string{`/bin/bash -lc "`, `/bin/bash -lc '`} {
		if rest, ok := strings.CutPrefix(c, p); ok && rest != "" {
			c = rest[:len(rest)-1]
		}
	}
	return strings.TrimSpace(c)
}

func offered(e event) bool {
	b, _ := e.Data["allow_in_thread"].(bool)
	return b
}

// nextApproval is the run's next approval; a run that finishes first fails the test.
func nextApproval(t *testing.T, ch <-chan event, runID, want string) event {
	t.Helper()
	for {
		e := next(t, ch, runID, "approval of "+want)
		switch e.Type {
		case "approval":
			return e
		case "run_finished":
			t.Fatalf("run %s finished before the approval of %q: %v", runID, want, e.Data)
		}
	}
}

// expectCard waits for the run's next approval and fails unless it is for want.
func expectCard(t *testing.T, ch <-chan event, runID, want string) event {
	t.Helper()
	e := nextApproval(t, ch, runID, want)
	if got := commandText(e); got != strings.TrimSpace(want) {
		t.Fatalf("run %s asked about %q, want %q (a card for a command that should have passed, or one that should have asked, is missing)", runID, got, strings.TrimSpace(want))
	}
	return e
}

func (s *server) answerCard(t *testing.T, e event, body map[string]any, want int) {
	t.Helper()
	s.do(t, "POST", "/api/approvals/"+asString(e.Data["id"]), body, nil, want)
}

type allowChange struct {
	Entity string `json:"entity"`
	Action string `json:"action"`
	After  struct {
		Command string `json:"command"`
	} `json:"after"`
}

func (s *server) allowed(t *testing.T, th thread) []string {
	t.Helper()
	var changes []allowChange
	s.do(t, "GET", "/api/threads/"+th.ID+"/changes", nil, &changes, http.StatusOK)
	var out []string
	for _, c := range changes {
		if c.Entity == "allow" && c.Action == "create" {
			out = append(out, c.After.Command)
		}
	}
	return out
}

// TestAllowInThread_PlainAnswersAskAgain: Allow and Deny answer one command once; the same command
// asks again. Passes before the feature: it proves the command-use scenario.
func TestAllowInThread_PlainAnswersAskAgain(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, commandPrompt(_cacheClean, _cacheClean, _cacheClean))
			s.answerCard(t, expectCard(t, ev, r.ID, _cacheClean), map[string]any{"approve": true}, http.StatusNoContent)
			s.answerCard(t, expectCard(t, ev, r.ID, _cacheClean), map[string]any{"approve": false}, http.StatusNoContent)
			s.answerCard(t, expectCard(t, ev, r.ID, _cacheClean), map[string]any{"approve": true}, http.StatusNoContent)
			got := s.finished(t, ev, r.ID)
			if want := replyLines(_cacheClean+": ran", _cacheClean+": denied", _cacheClean+": ran"); got.Reply != want {
				t.Errorf("reply %q, want %q", got.Reply, want)
			}
			if a := s.allowed(t, th); len(a) != 0 {
				t.Errorf("plain answers recorded allows %q", a)
			}
		})
	}
}

// TestAllowInThread_SameCommandPasses: choosing Allow in this thread lets the exact same command
// through without a card for the rest of the thread, runs after this one included; any other
// command, and the same command in another thread, still asks. The thread records one change.
func TestAllowInThread_SameCommandPasses(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			ev := s.events(t, th)

			// Same run: the first ask is answered in-thread, the second is the same command.
			r := s.send(t, th, commandPrompt(_cacheClean, _cacheClean, _tmpClean))
			e := expectCard(t, ev, r.ID, _cacheClean)
			if !offered(e) {
				t.Fatalf("approval %v does not offer Allow in this thread (allow_in_thread)", e.Data)
			}
			s.answerCard(t, e, map[string]any{"approve": true, "in_thread": true}, http.StatusNoContent)
			s.answerCard(t, expectCard(t, ev, r.ID, _tmpClean), map[string]any{"approve": true}, http.StatusNoContent)
			got := s.finished(t, ev, r.ID)
			if want := replyLines(_cacheClean+": ran", _cacheClean+": ran", _tmpClean+": ran"); got.Reply != want {
				t.Errorf("reply %q, want %q", got.Reply, want)
			}
			if a := s.allowed(t, th); len(a) != 1 || a[0] != _cacheClean {
				t.Errorf("the thread's allows %q, want one: %q", a, _cacheClean)
			}

			// A later run of the thread: the allow still holds, the plain answer to _tmpClean did not stick.
			r = s.send(t, th, commandPrompt(_tmpClean, _cacheClean))
			s.answerCard(t, expectCard(t, ev, r.ID, _tmpClean), map[string]any{"approve": true}, http.StatusNoContent)
			got = s.finished(t, ev, r.ID)
			if want := replyLines(_tmpClean+": ran", _cacheClean+": ran"); got.Reply != want {
				t.Errorf("second run reply %q, want %q", got.Reply, want)
			}
			if a := s.allowed(t, th); len(a) != 1 {
				t.Errorf("the thread's allows after silent passes %q, want one", a)
			}

			// Another thread of the same Bot has no allow.
			other := s.newThread(t, b)
			oev := s.events(t, other)
			r = s.send(t, other, commandPrompt(_cacheClean))
			s.answerCard(t, expectCard(t, oev, r.ID, _cacheClean), map[string]any{"approve": true}, http.StatusNoContent)
			s.finished(t, oev, r.ID)
			if a := s.allowed(t, other); len(a) != 0 {
				t.Errorf("the other thread's allows %q, want none", a)
			}
		})
	}
}

// TestAllowInThread_SameMeansSameText: the command is trimmed, then equal byte for byte.
func TestAllowInThread_SameMeansSameText(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			spaced := "rm  -rf /tmp/codex-gocache-161"
			r := s.send(t, th, commandPrompt(_cacheClean, "  "+_cacheClean+" \n", spaced, "RM -rf /tmp/codex-gocache-161"))
			first := expectCard(t, ev, r.ID, _cacheClean)
			if !offered(first) {
				t.Fatalf("approval %v does not offer Allow in this thread (allow_in_thread)", first.Data)
			}
			s.answerCard(t, first, map[string]any{"approve": true, "in_thread": true}, http.StatusNoContent)
			s.answerCard(t, expectCard(t, ev, r.ID, spaced), map[string]any{"approve": true}, http.StatusNoContent)
			s.answerCard(t, expectCard(t, ev, r.ID, "RM -rf /tmp/codex-gocache-161"), map[string]any{"approve": true}, http.StatusNoContent)
			s.finished(t, ev, r.ID)
		})
	}
}

// TestAllowInThread_NotOffered: a command with push, delete, -rf outside the worktree and tmp
// roots, or a gh write is never offered; asking for it anyway answers 400 and leaves the approval
// open. Ordinary commands, rm -rf in a tmp root and gh reads are offered.
func TestAllowInThread_NotOffered(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cmd     string
		offered bool
	}{
		{_cacheClean, true},
		{_cacheClean + " /tmp/codex-gotmp-161", true},
		{"go clean -cache", true},
		{"gh pr view 12", true},
		{"gh pr list --state open", true},
		{"gh api repos/o/r/pulls/12", true},
		{"git push origin fix/x", false},
		{"git push --force-with-lease", false},
		{"gh repo delete o/r --yes", false},
		{"rm -rf /var/lib/reef-nope", false},
		{"rm -fr /tmp/codex-gocache-161 /var/lib/reef-nope", false},
		{"rm -rf /tmp/../etc", false},
		{"rm -rf ~/.cache/x", false},
		{"rm -rf build", false},
		{"rm -rf /tmp/*", false},
		{"gh pr create --title x --body y", false},
		{"gh pr merge 12 --squash", false},
		{"gh issue comment 3 --body hi", false},
		{"gh api -X POST repos/o/r/issues -f title=x", false},
		{"gh api repos/o/r/issues -f title=x", false},
		{"gh workflow run ci.yml", false},
		{"cd /tmp/x && gh pr edit 3 --title y", false},
	}
	var cmds []string
	for _, c := range cases {
		cmds = append(cmds, c.cmd)
	}
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, commandPrompt(cmds...))
			var wrong []string
			for i, c := range cases {
				e := expectCard(t, ev, r.ID, c.cmd)
				if offered(e) != c.offered {
					wrong = append(wrong, c.cmd)
				}
				var open struct {
					Approval *struct {
						ID      string `json:"id"`
						Offered bool   `json:"allow_in_thread"`
					} `json:"approval"`
				}
				s.do(t, "GET", "/api/threads/"+th.ID, nil, &open, http.StatusOK)
				if open.Approval == nil || open.Approval.ID != asString(e.Data["id"]) || open.Approval.Offered != c.offered {
					t.Errorf("GET /api/threads/%s approval %+v for %q, want id %v and allow_in_thread %v", th.ID, open.Approval, c.cmd, e.Data["id"], c.offered)
				}
				if !c.offered {
					s.answerCard(t, e, map[string]any{"approve": true, "in_thread": true}, http.StatusBadRequest)
				} else if i == 0 {
					s.answerCard(t, e, map[string]any{"approve": false, "in_thread": true}, http.StatusBadRequest)
				}
				s.answerCard(t, e, map[string]any{"approve": true}, http.StatusNoContent)
			}
			if len(wrong) > 0 {
				t.Errorf("allow_in_thread differs from the design for: %q", wrong)
			}
			s.finished(t, ev, r.ID)
			if a := s.allowed(t, th); len(a) != 0 {
				t.Errorf("refused or plain answers recorded allows %q", a)
			}
		})
	}
}
