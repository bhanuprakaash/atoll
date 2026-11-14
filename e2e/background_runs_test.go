package e2e

import (
	"net/http"
	"strings"
	"testing"
)

// Contract: no-report-contract.md, "Background runs".

// The helper plays testdata/*/helper-blocker.jsonl: it answers at once, `_blockerReply`. The boss
// woken with that reply plays testdata/*/wake-tell.jsonl: two tell_user calls, then a final message.
const (
	_blockerPrompt = "Run the checks, then report the blocker."
	_blockerReply  = "Tester here: blocked on the release notes."
	_toldFirst     = "Tester is blocked: it cannot read the release notes."
	_toldSecond    = "I will hold the changelog until you say how to get it access."
	_toldFinal     = "Told the user about the blocker."
)

// _backgroundLine is the line Reef starts a background run's prompt with, for the user Bhanu.
const _backgroundLine = "Reef: background run. Bhanu does not see your final message, so end with one line of status. " +
	"If Bhanu must act (a PR to merge, a decision, a failure you cannot fix), tell them with tell_user."

// backgroundSetup serves atoll with the user named Bhanu and a boss, Poteto, that delegates to Tester.
func backgroundSetup(t *testing.T, p string) (*machine, *server, bot, thread, <-chan event) {
	t.Helper()
	m, s := start(t, p)
	s.do(t, "PUT", "/api/settings", map[string]string{"user_name": "Bhanu", "worktree_root": t.TempDir()}, nil, http.StatusNoContent)
	poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, poteto, tester)
	boss := s.newThread(t, poteto)
	return m, s, tester, boss, s.events(t, boss)
}

// botMessagesOf are the texts of the Bot messages a run added to the chat.
func botMessagesOf(t *testing.T, s *server, threadID, runID string) []string {
	t.Helper()
	var got []string
	for _, m := range bossMessages(t, s, threadID) {
		if m.Role == "bot" && m.RunID == runID {
			got = append(got, m.Text)
		}
	}
	return got
}

// A run that starts only with a helper's notice adds no chat message and does not make the thread
// Replied: the person had opened it, and it is not New after the run.
func TestBackgroundRun_HelperNoticeAddsNoMessage(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s, tester, boss, ev := backgroundSetup(t, p)
			ids, _ := delegateTester(t, s, boss, ev, _reportPrompt)
			before := len(bossMessages(t, s, boss.ID))
			s.do(t, "POST", "/api/threads/"+boss.ID+"/opened", nil, nil, http.StatusNoContent)

			helpersEnd(t, s, ids[0])
			msg, _, wake := woken(t, m, s, p, boss.ID, tester.ID, 1)
			if wake.Status != "done" {
				t.Fatalf("boss run for the notice: status %q, error %q; want done", wake.Status, wake.Error)
			}
			if !strings.HasPrefix(msg.Text, "Reef: Tester finished thread "+ids[0]+":") {
				t.Errorf("notice %q, want it to start with the Reef line for thread %s", msg.Text, ids[0])
			}
			if got := botMessagesOf(t, s, boss.ID, wake.ID); len(got) != 0 {
				t.Errorf("the notice's run added the Bot messages %q, want none: its final message is a status line", got)
			}
			if n := len(bossMessages(t, s, boss.ID)); n != before+1 {
				t.Errorf("boss thread has %d messages after the notice, want the %d it had plus the notice itself", n, before+1)
			}
			if got := sidebarState(t, s, boss.BotID, boss.ID); got != "" {
				t.Errorf("boss thread state %q after a background run, want none (not Replied)", got)
			}
		})
	}
}

// A background run's prompt starts with the background line, the run lists tell_user and not
// finish_run, and each tell_user call adds one Bot message and makes the thread Replied; the final
// message is no message.
func TestBackgroundRun_TellUserReachesThePerson(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s, tester, boss, ev := backgroundSetup(t, p)
			ids, _ := delegateTester(t, s, boss, ev, _blockerPrompt)
			s.do(t, "POST", "/api/threads/"+boss.ID+"/opened", nil, nil, http.StatusNoContent)
			if h := settled(t, s, ids[0]); h.Status != "done" || h.Reply != _blockerReply {
				t.Fatalf("helper run: status %q, reply %q, error %q; want done, %q", h.Status, h.Reply, h.Error, _blockerReply)
			}

			_, _, wake := woken(t, m, s, p, boss.ID, tester.ID, 1)
			if wake.Status != "done" {
				t.Fatalf("boss run for the notice: status %q, error %q; want done", wake.Status, wake.Error)
			}
			prompts := promptsWith(t, m, p, _finishedThread)
			if len(prompts) != 1 {
				t.Fatalf("%d boss prompts hold the notice, want 1: %q", len(prompts), prompts)
			}
			if first, rest, _ := strings.Cut(prompts[0], "\n"); first != _backgroundLine || !strings.Contains(rest, finishedNotice("Tester", ids[0])) {
				t.Errorf("background prompt %q, want first line %q, then the notice", prompts[0], _backgroundLine)
			}
			assertTools(t, "background", toolsOf(t, m, p, func(prompt string) bool { return strings.HasPrefix(prompt, _backgroundLine) }),
				[]string{"tell_user", "delegate", "continue", "remember"}, []string{"finish_run"})

			if got := botMessagesOf(t, s, boss.ID, wake.ID); len(got) != 2 || got[0] != _toldFirst || got[1] != _toldSecond {
				t.Errorf("Bot messages of the background run %q, want one per tell_user call: %q then %q (not the final message %q)", got, _toldFirst, _toldSecond, _toldFinal)
			}
			if got := sidebarState(t, s, boss.BotID, boss.ID); got != "new" {
				t.Errorf("boss thread state %q after tell_user, want new (Replied)", got)
			}
		})
	}
}
