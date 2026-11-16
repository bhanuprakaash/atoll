package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// TestWakeHelpersLine_HeldWakeCountsNewHelper: a helper's run ends while its boss's run is going, so
// the wake is held. The boss starts another helper before the wake is delivered: the wake's helpers
// line counts that helper, as the line is worked out when the wake is delivered, not when the
// helper ended.
//
// The boss's second run waits on an approval of a library server's tool between starting nothing
// and starting the second helper, which gives the first helper time to end inside it.
func TestWakeHelpersLine_HeldWakeCountsNewHelper(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newMCPFixture(t, p)
			s := f.s
			poteto, tester := f.bot(t, "Poteto", f.local.ID), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			first := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _reportPrompt, ""))[0])

			payload, _ := json.Marshal([]fake.MCPCall{
				mcpCall("docs-local", "answer", `{}`), delegateCall("Tester", _reviews[0], ""),
			})
			busy := s.send(t, boss, _mcpPromptHead+string(payload))
			approval := nextOf(t, ev, busy.ID, "approval")

			allow(t, s, first)
			if r := settled(t, s, first); r.Status != "done" {
				t.Fatalf("first helper %s: status %q, error %q; want done", first, r.Status, r.Error)
			}
			if got := fromBot(t, s, boss.ID, tester.ID); len(got) != 0 {
				t.Fatalf("boss run %s waits on an approval, yet the boss thread got %d messages from Tester %+v, want the wake held", busy.ID, len(got), got)
			}

			s.do(t, "POST", "/api/approvals/"+approval, map[string]bool{"approve": true}, nil, http.StatusNoContent)
			r := s.finished(t, ev, busy.ID)
			lines := strings.Split(r.Reply, "\n")
			if r.Status != "done" || len(lines) != 2 {
				t.Fatalf("boss run %s: status %q, error %q, reply %q; want done and two lines", r.ID, r.Status, r.Error, r.Reply)
			}
			second := delegated(t, lines[1])
			helperRunning(t, s, second)

			msg, _, _ := woken(t, f.m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "held wake", msg.Text, "Tester finished thread "+first+":", _reportReply)
			want := helpersLine(onThread(t, s, "Tester", second))
			if !strings.HasSuffix(msg.Text, "\n\n"+want) {
				t.Errorf("wake held while the boss started helper %s: %q\nwant it to end with %q (helper %s was running when the wake was delivered)",
					second, msg.Text, want, second)
			}
		})
	}
}

// TestWakeHelpersLine_AtOnce: a wake delivered the moment the helper's run ends, to an idle boss,
// ends with the helpers line of the helpers going then: the one that ended is not counted.
func TestWakeHelpersLine_AtOnce(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev,
				delegateCall("Tester", _reviews[0], ""), delegateCall("Tester", _reportPrompt, ""))
			if len(lines) != 2 {
				t.Fatalf("two delegates: reply %q, want two lines", lines)
			}
			review, reporter := delegated(t, lines[0]), delegated(t, lines[1])
			helperRunning(t, s, review)

			allow(t, s, reporter)
			msg, _, _ := woken(t, m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "wake", msg.Text, "Tester finished thread "+reporter+":", _reportReply)
			endsWithLine(t, "wake", msg.Text, helpersLine(onThread(t, s, "Tester", review)))
		})
	}
}
