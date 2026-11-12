package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Contract: titles-contract.md → Label prefixes and cut titles.

// _plainPrompt is a recorded prompt that holds no title trigger: its title session fails.
const _plainPrompt = "Reply with exactly: pong"

// _titleFirstWords is the first-words title of a message that starts with _titlePrompt.
const _titleFirstWords = "Name this thread: the login redirect"

// TestTitles_AnswerRules: the title session's raw answer becomes the title without a label prefix,
// and a cut answer never ends on a stop word. An empty want means no title: the first words stay.
func TestTitles_AnswerRules(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, raw, want string }{
		{"conversation title label", "Conversation title: Login redirect loop on settings", "Login redirect loop on settings"},
		{"title label", "Title: Fix flaky keychain test", "Fix flaky keychain test"},
		{"thread title label", "Thread title: Fix flaky keychain test", "Fix flaky keychain test"},
		{"chat title label", "Chat title: Fix flaky keychain test", "Fix flaky keychain test"},
		{"status line label", "Status line: Waiting on review", "Waiting on review"},
		{"bold label", "**Title:** Login redirect loop", "Login redirect loop"},
		{"lower case label", "title: login redirect loop", "Login redirect loop"},
		{"label is not counted in the six words", "Conversation title: Login redirect loop on settings page today", "Login redirect loop on settings page"},
		{"only a label", "Title:", ""},
		{"other colon stays", "Fix: login redirect loop", "Fix: login redirect loop"},
		{"cut on on", "Final review of #122 fix on the thing", "Final review of #122 fix"},
		{"cut on and", "Keychain save failures in sign-in and token expiry", "Keychain save failures in sign-in"},
		{"cut on several stop words", "Notes on the state of the API", "Notes on the state"},
		{"cut on to", "Move the sidebar rows to a grid", "Move the sidebar rows"},
		{"cut on for", "Sign-in retry limits and caps for tokens", "Sign-in retry limits and caps"},
		{"cut on with", "Fix the login loop caused with cookies", "Fix the login loop caused"},
		{"six words are not cut", "Where does this setting belong to", "Where does this setting belong to"},
		{"nothing left", "On the of and to in a for", ""},
	}
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			bot := s.newBot(t, "Ada", p)
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					th := s.newThread(t, bot)
					s.finished(t, s.events(t, th), s.send(t, th, _titlePrompt+" "+fake.TitleAnswer(c.raw)).ID)
					if c.want == "" {
						titleFailed(t, s, th.ID)
						if got := s.thread(t, th.ID).Title; got != _titleFirstWords {
							t.Errorf("answer %q: title %q, want no title and the first words %q", c.raw, got, _titleFirstWords)
						}
						return
					}
					titled(t, s, th.ID, c.want)
				})
			}
		})
	}
}

// TestTitles_FirstWordsRules: a first-words title, kept when the title session fails, never ends
// on a stop word either when the message was cut, and keeps the user's own words.
func TestTitles_FirstWordsRules(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, message, want string }{
		{"cut on the", "hey we have to bring the shell tests back.", "hey we have to bring"},
		{"cut on several stop words", "Please review the plan for the release today.", "Please review the plan"},
		{"cut on and", "Keychain save failures in sign-in and token expiry.", "Keychain save failures in sign-in"},
		{"label stays", "Status: why does login loop on settings page", "Status: why does login loop"},
		{"cut ends on a word", "Why does the login redirect loop on settings.", "Why does the login redirect loop"},
	}
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			bot := s.newBot(t, "Ada", p)
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					th := s.newThread(t, bot)
					run := s.send(t, th, c.message+" "+_plainPrompt)
					if got := s.thread(t, th.ID).Title; got != c.want {
						t.Errorf("message %q: title %q when sent, want %q", c.message, got, c.want)
					}
					s.finished(t, s.events(t, th), run.ID)
					titleFailed(t, s, th.ID)
					if got := s.thread(t, th.ID).Title; got != c.want {
						t.Errorf("message %q: title %q after the failed title session, want %q", c.message, got, c.want)
					}
				})
			}
		})
	}
}

// titleFailed waits until the server logs that naming the thread failed, so the title session had
// its chance to change the title.
func titleFailed(t *testing.T, s *server, threadID string) {
	t.Helper()
	eventually(t, 30*time.Second, "the failed title session of thread "+threadID, func() bool {
		return strings.Contains(s.log.String(), `"msg":"name thread","thread_id":"`+threadID+`"`)
	})
}

// TestTitles_LaunchCleansStoredTitles: on the next launch a stored title loses its label and a
// six-word title cut on a stop word loses it; every other title stays.
func TestTitles_LaunchCleansStoredTitles(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	bot := s.newBot(t, "Ada", "claude")
	stored := []struct{ old, want string }{
		{"Conversation title: Login redirect loop", "Login redirect loop"},
		{"Title: Fix flaky keychain test", "Fix flaky keychain test"},
		{"Status line: Waiting on review", "Waiting on review"},
		{"Final review of #122 fix on", "Final review of #122 fix"},
		{"Keychain save failures in sign-in and", "Keychain save failures in sign-in"},
		{"What is this for", "What is this for"},
		{"Fix: login redirect loop", "Fix: login redirect loop"},
		{"Login redirect loop", "Login redirect loop"},
	}
	ids := make([]string, len(stored))
	for i := range stored {
		ids[i] = s.newThread(t, bot).ID
	}
	s.kill()
	for i, c := range stored {
		dbExec(t, m, fmt.Sprintf(`UPDATE threads SET title = '%s' WHERE id = '%s'`, c.old, ids[i]))
	}

	s = m.serve(t)
	for i, c := range stored {
		if got := s.thread(t, ids[i]).Title; got != c.want {
			t.Errorf("stored title %q after a launch: %q, want %q", c.old, got, c.want)
		}
	}
}
