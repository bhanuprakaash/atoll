package e2e

// Contract: approvals-contract.md.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// TestApproval_ClosedOnAnswer: answering an approval (deny or allow) publishes approval_closed
// {run_id, id} on the thread's stream. A stream opened mid-run, with one approval answered and
// one open, leaves only the open one: a replayed approval is followed by its approval_closed.
func TestApproval_ClosedOnAnswer(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newMCPFixture(t, p)
			th := f.s.newThread(t, f.bot(t, "Kelp", f.local.ID))
			ev := f.s.events(t, th)
			call := mcpCall("docs-local", "answer", `{}`)
			payload, _ := json.Marshal([]fake.MCPCall{call, call})
			r := f.s.send(t, th, _mcpPromptHead+string(payload))

			first := nextOf(t, ev, r.ID, "approval")
			f.s.do(t, "POST", "/api/approvals/"+first, map[string]bool{"approve": false}, nil, http.StatusNoContent)
			second, closed := "", false
			for second == "" || !closed {
				e := next(t, ev, r.ID, fmt.Sprintf("second approval (got %q) and approval_closed of %s (got %v)", second, first, closed))
				id := asString(e.Data["id"])
				switch e.Type {
				case "approval":
					if id == first {
						t.Fatalf("approval %s came again after it was answered", first)
					}
					second = id
				case "approval_closed":
					if id != first {
						t.Fatalf("approval_closed %v, want id %s (the answered approval)", e.Data, first)
					}
					closed = true
				case "run_finished":
					t.Fatalf("run %s finished before its second approval: %v", r.ID, e.Data)
				}
			}

			// Reconnect mid-run: what the new stream replays leaves only the open approval.
			again := f.s.events(t, th)
			if open := openApprovals(t, again, r.ID, second); !slices.Equal(open, []string{second}) {
				t.Errorf("open approvals after a reconnect %q, want only %q (answered %q)", open, second, first)
			}

			f.s.do(t, "POST", "/api/approvals/"+second, map[string]bool{"approve": true}, nil, http.StatusNoContent)
			for _, ch := range []<-chan event{ev, again} {
				if id := nextOf(t, ch, r.ID, "approval_closed"); id != second {
					t.Errorf("approval_closed id %q after answering %s", id, second)
				}
			}
			got := f.s.finished(t, ev, r.ID)
			if want := replyLines("docs-local/answer: denied", "docs-local/answer: stdio-answer-42"); got.Reply != want {
				t.Errorf("reply %q, want %q", got.Reply, want)
			}
			f.s.do(t, "POST", "/api/approvals/"+first, map[string]bool{"approve": true}, nil, http.StatusNotFound)
		})
	}
}

// TestApproval_ClosedOnStop: an approval dropped because its run ended is closed on the stream.
func TestApproval_ClosedOnStop(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _writePrompt)
			id := nextOf(t, ev, r.ID, "approval")

			s.do(t, "POST", "/api/runs/"+r.ID+"/stop", nil, nil, http.StatusNoContent)
			var closed, finished bool
			grace := time.After(30 * time.Second)
			for !closed {
				select {
				case e, ok := <-ev:
					if !ok {
						t.Fatalf("event stream closed before approval_closed of %s", id)
					}
					if e.Data["run_id"] != r.ID {
						continue
					}
					switch e.Type {
					case "approval_closed":
						if got := asString(e.Data["id"]); got != id {
							t.Fatalf("approval_closed id %q, want %q", got, id)
						}
						closed = true
					case "run_finished":
						finished = true
						grace = time.After(4 * _pace)
					}
				case <-grace:
					t.Fatalf("no approval_closed for %s after stopping run %s (run_finished seen: %v)", id, r.ID, finished)
				}
			}
		})
	}
}

// TestApproval_ClaudeToolTimeout: Claude Code cancels an MCP tool call after 60 seconds unless
// MCP_TOOL_TIMEOUT says otherwise, and Reef's permission prompt is such a call. A run gives
// claude MCP_TOOL_TIMEOUT of at least 10 minutes, in milliseconds.
func TestApproval_ClaudeToolTimeout(t *testing.T) {
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	s.finished(t, s.events(t, th), s.send(t, th, _keepPrompt).ID)
	calls := m.sessions(t, "claude")
	if len(calls) == 0 {
		t.Fatal("claude ran no session")
	}
	v := calls[len(calls)-1].env["MCP_TOOL_TIMEOUT"]
	if ms, err := strconv.Atoi(v); err != nil || ms < 600_000 {
		t.Errorf("claude env MCP_TOOL_TIMEOUT=%q, want milliseconds >= 600000 (10 minutes)", v)
	}
}

// next is the run's next event; want says what the test waits for.
func next(t *testing.T, ch <-chan event, runID, want string) event {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("event stream closed waiting for %s of run %s", want, runID)
			}
			if e.Data["run_id"] == runID {
				return e
			}
		case <-timeout:
			t.Fatalf("no event of run %s in 30s, waiting for %s", runID, want)
		}
	}
}

// nextOf skips to the run's next event of typ and returns its id.
func nextOf(t *testing.T, ch <-chan event, runID, typ string) string {
	t.Helper()
	for {
		e := next(t, ch, runID, typ)
		if e.Type == typ {
			return asString(e.Data["id"])
		}
		if e.Type == "run_finished" {
			t.Fatalf("run %s finished before %s: %v", runID, typ, e.Data)
		}
	}
}

// openApprovals reads a fresh stream until approval want arrives and the stream is quiet, and
// returns the approvals it opened and did not close, in order. A close must follow its approval.
func openApprovals(t *testing.T, ch <-chan event, runID, want string) []string {
	t.Helper()
	var open []string
	seen := false
	timeout := time.After(30 * time.Second)
	for {
		var quiet <-chan time.Time
		if seen {
			quiet = time.After(4 * _pace)
		}
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("event stream closed during replay of run %s", runID)
			}
			if e.Data["run_id"] != runID {
				continue
			}
			id := asString(e.Data["id"])
			switch e.Type {
			case "approval":
				open = append(open, id)
				seen = seen || id == want
			case "approval_closed":
				i := slices.Index(open, id)
				if i < 0 {
					t.Fatalf("replay sent approval_closed %s before (or without) its approval", id)
				}
				open = slices.Delete(open, i, i+1)
			case "run_finished":
				t.Fatalf("run %s finished during replay: %v", runID, e.Data)
			}
		case <-quiet:
			return open
		case <-timeout:
			t.Fatalf("replay of run %s never sent approval %s; open %q", runID, want, open)
		}
	}
}
