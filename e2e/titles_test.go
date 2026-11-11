package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Contract: titles-contract.md.

const (
	_titlePrompt = "Name this thread: " + fake.TitleTrigger + ". Reply with just: ok"
	_titleWant   = "Login redirect loop on settings"
)

var _titleModel = map[string]string{"claude": "haiku", "codex": "gpt-6-luna"}

type briefMessage struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	RunID     string `json:"run_id"`
	FromBotID string `json:"from_bot_id"`
}

// titled waits up to 15s for the thread's title to be want.
func titled(t *testing.T, s *server, id, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got := s.thread(t, id).Title
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("thread %s title %q after 15s, want %q", id, got, want)
		}
		time.Sleep(_pace)
	}
}

// TestTitles_Generated: after the first reply the provider's cheapest model titles the thread, in
// a fresh session of its own that is no run and sends nothing on the thread's stream.
func TestTitles_Generated(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			run := s.finished(t, ev, s.send(t, th, _titlePrompt).ID)
			titled(t, s, th.ID, _titleWant)

			var msgs []message
			s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &msgs, http.StatusOK)
			if len(msgs) != 2 || msgs[0].RunID != run.ID || msgs[1].RunID != run.ID {
				t.Errorf("messages %+v, want the user message and the reply of run %s only", msgs, run.ID)
			}
			if got := s.thread(t, th.ID); got.RunID != run.ID || got.RunStatus != "done" {
				t.Errorf("thread run %s %q, want run %s done", got.RunID, got.RunStatus, run.ID)
			}
			select {
			case e := <-ev:
				t.Errorf("thread stream sent %s %v after the run finished, want nothing for the title", e.Type, e.Data)
			default:
			}

			titles := m.titles(t, p)
			if len(titles) != 1 {
				t.Fatalf("%d title sessions, want 1", len(titles))
			}
			c := titles[0]
			switch p {
			case "claude":
				if got := flagValue(c.argv, "--setting-sources"); got != "project" {
					t.Errorf("claude title --setting-sources %q, want project; argv %q", got, c.argv)
				}
				if i := slices.Index(c.argv, "--model"); i < 0 || c.argv[i+1] != _titleModel[p] ||
					slices.Contains(c.argv, "--resume") || slices.Contains(c.argv, "--mcp-config") {
					t.Errorf("title session argv %q, want --model %s, no --resume, no --mcp-config", c.argv, _titleModel[p])
				}
			case "codex":
				start := slices.IndexFunc(c.stdin, func(l string) bool { return strings.Contains(l, `"thread/start"`) })
				if start < 0 || !strings.Contains(c.stdin[start], `"model":"`+_titleModel[p]+`"`) || request(c, "thread/resume") != nil {
					t.Errorf("title session did not start a fresh thread on %s; stdin:\n%s", _titleModel[p], strings.Join(c.stdin, "\n"))
				}
			}
		})
	}
}

// TestTitles_UserTitleKept: a title the user set is never replaced, so no title session runs.
func TestTitles_UserTitleKept(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	s.do(t, "PATCH", "/api/threads/"+th.ID, map[string]string{"title": "My plan"}, nil, http.StatusOK)
	s.finished(t, s.events(t, th), s.send(t, th, _titlePrompt).ID)
	time.Sleep(2 * time.Second)
	if got := s.thread(t, th.ID).Title; got != "My plan" {
		t.Errorf("title %q after the first reply, want the user's %q", got, "My plan")
	}
	if n := len(m.titles(t, "claude")); n != 0 {
		t.Errorf("%d title sessions for a thread the user titled, want 0", n)
	}
}

// TestTitles_HelperBrief: a helper thread's brief is a message from the boss, and the helper
// thread's title comes from the brief, never from the "delegated this to you" preface: first
// words until a reply, then a title from a session that saw no preface.
func TestTitles_HelperBrief(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)

			hang := s.newThread(t, poteto)
			lines := useAtoll(t, s, hang, s.events(t, hang), delegateCall("Tester", _hangPrompt, ""))
			helper := delegated(t, lines[0])
			if got, want := s.thread(t, helper).Title, threadTitle(_hangPrompt); got != want {
				t.Errorf("helper thread title %q before a reply, want the brief's first words %q", got, want)
			}
			var msgs []briefMessage
			s.do(t, "GET", "/api/threads/"+helper+"/messages", nil, &msgs, http.StatusOK)
			if len(msgs) == 0 || msgs[0].Role != "user" || msgs[0].FromBotID != poteto.ID {
				t.Errorf("helper thread messages %+v, want the brief first with from_bot_id %s (Poteto)", msgs, poteto.ID)
			}

			named := s.newThread(t, poteto)
			lines = useAtoll(t, s, named, s.events(t, named), delegateCall("Tester", _titlePrompt, ""))
			titled(t, s, delegated(t, lines[0]), _titleWant)
			for _, c := range m.titles(t, p) {
				if slices.ContainsFunc(c.stdin, func(l string) bool { return strings.Contains(l, "delegated this to you") }) {
					t.Errorf("a title session saw the helper preface; stdin:\n%s", strings.Join(c.stdin, "\n"))
				}
			}
		})
	}
}
