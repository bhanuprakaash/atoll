package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestRun_StatusWords(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	b := chief(t, s).bot
	th := s.newThread(t, b)
	ev := s.events(t, th)
	r := s.send(t, th, "Show status words for commands and platform tools.")
	want := []struct {
		name, activity string
		background     bool
	}{
		{"Bash", "Running make check", false},
		{"Bash", "Waiting on a background task", true},
		{"Bash", "Waiting on a background task", true},
		{"Bash", "Waiting on a background task", true},
		{"Bash", "Waiting on a background task", true},
		{"Bash", "Running cat", false},
		{"mcp__atoll__list_bots", "Looking around", false},
		{"mcp__atoll__list_skills", "Looking around", false},
		{"mcp__atoll__create_bot", "Setting up Example", false},
	}
	for i, w := range want {
		e := until(t, ev, r.ID, "tool_call")
		data := e[len(e)-1].Data
		name, _ := data["name"].(string)
		verb, _ := data["verb"].(string)
		target, _ := data["target"].(string)
		activity := strings.TrimSpace(verb + " " + target)
		if name != w.name || activity != w.activity {
			t.Errorf("tool %d: %q activity %q, want %q activity %q", i, name, activity, w.name, w.activity)
		}
		if w.background && strings.Contains(activity, ".output") {
			t.Errorf("status line exposes output file: %q", activity)
		}
		if i == 3 || i == 4 {
			activity = s.bot(t, b.ID).Activity
			if strings.Contains(activity, ".output") {
				t.Errorf("sidebar exposes output file: %q", activity)
			}
			if activity != w.activity {
				t.Errorf("sidebar activity %q, want %q", activity, w.activity)
			}
		}
	}
	for _, e := range until(t, ev, r.ID, "run_finished") {
		if e.Type != "tool_call" || e.Data["name"] != "mcp__atoll__finish_run" {
			continue
		}
		verb, _ := e.Data["verb"].(string)
		target, _ := e.Data["target"].(string)
		activity := strings.TrimSpace(verb + " " + target)
		if strings.HasPrefix(activity, "Setting up") {
			t.Errorf("finish_run activity = %q", activity)
		}
	}
	t.Run("codex", func(t *testing.T) {
		_, s := start(t, "codex")
		th := s.newThread(t, s.newBot(t, "Ada", "codex"))
		ev := s.events(t, th)
		r := s.send(t, th, "Show Codex command status words.")
		want := []string{"Running make", "Running node", "Running make", "Running make", "Running make"}
		for i, w := range want {
			e := until(t, ev, r.ID, "tool_call")
			data := e[len(e)-1].Data
			verb, _ := data["verb"].(string)
			target, _ := data["target"].(string)
			activity := strings.TrimSpace(verb + " " + target)
			if data["name"] != "commandExecution" || activity != w {
				t.Errorf("Codex tool %d: %q activity %q, want commandExecution activity %q", i, data["name"], activity, w)
			}
		}
		until(t, ev, r.ID, "run_finished")
	})

}

// liveWords is what a thread's API shows of its run now: the live verb (sidebar) and step (Helpers line).
type liveWords struct {
	Verb    string `json:"verb"`
	Step    string `json:"step"`
	Helpers []struct {
		Step string `json:"step"`
	} `json:"helpers"`
}

func (s *server) liveWords(t *testing.T, id string) liveWords {
	t.Helper()
	var w liveWords
	s.do(t, "GET", "/api/threads/"+id, nil, &w, 200)
	return w
}

// TestRun_StatusWordsShell: a shell fragment never names the line; the first real program does.
// The recordings pause after each command, so the API still shows it when its event arrives.
func TestRun_StatusWordsShell(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			ev := s.events(t, th)
			prompt := map[string]string{"claude": "Show status words for shell fragments.", "codex": "Show Codex shell fragment status words."}[p]
			r := s.send(t, th, prompt)
			want := []string{"Running gh pr", "Running gh pr", "Running gh pr", "Running cat", "Running git log", "Running make check", "Running make e2e"}
			if p == "codex" {
				want = []string{"Running gh", "Running gh", "Running gh", "Running cat", "Running git", "Running make", "Running make"}
			}
			for i, w := range want {
				e := until(t, ev, r.ID, "tool_call")
				data := e[len(e)-1].Data
				verb, _ := data["verb"].(string)
				target, _ := data["target"].(string)
				if got := strings.TrimSpace(verb + " " + target); got != w {
					t.Errorf("command %d: event %q, want %q", i, got, w)
				}
				if got := s.bot(t, b.ID).Activity; got != w {
					t.Errorf("command %d: sidebar activity %q, want %q", i, got, w)
				}
				if got := s.liveWords(t, th.ID); got.Verb != w || got.Step != w {
					t.Errorf("command %d: thread verb %q step %q, want both %q", i, got.Verb, got.Step, w)
				}
			}
			until(t, ev, r.ID, "run_finished")
		})
	}
}

// TestRun_StatusWordsHandoff: delegate and continue read "Handing off", never "Setting up", in the
// event, the sidebar and a helper's Helpers line.
func TestRun_StatusWordsHandoff(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	const handoff = "Hand the work off to a helper."

	lead := s.newThread(t, s.newBot(t, "Lead", "claude"))
	ev := s.events(t, lead)
	r := s.send(t, lead, handoff)
	for _, name := range []string{"mcp__atoll__delegate", "mcp__atoll__continue"} {
		e := until(t, ev, r.ID, "tool_call")
		data := e[len(e)-1].Data
		verb, _ := data["verb"].(string)
		target, _ := data["target"].(string)
		if data["name"] != name || verb != "Handing off" || target != "" {
			t.Errorf("%v: event verb %q target %q, want %s with verb %q and no target", data["name"], verb, target, name, "Handing off")
		}
		if got := s.bot(t, lead.BotID).Activity; got != "Handing off" {
			t.Errorf("%s: sidebar activity %q, want %q", name, got, "Handing off")
		}
		if got := s.liveWords(t, lead.ID); got.Verb != "Handing off" || got.Step != "Handing off" {
			t.Errorf("%s: thread verb %q step %q, want both %q", name, got.Verb, got.Step, "Handing off")
		}
	}

	boss, tester := s.newBot(t, "Poteto", "claude"), s.newBot(t, "Tester", "claude")
	setDelegates(t, s, boss, tester)
	bossThread := s.newThread(t, boss)
	useAtoll(t, s, bossThread, s.events(t, bossThread), delegateCall("Tester", handoff, ""))
	var step string
	for deadline := time.Now().Add(15 * time.Second); step == "" && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if h := s.liveWords(t, bossThread.ID).Helpers; len(h) == 1 {
			step = h[0].Step
		}
	}
	if step != "Handing off" {
		t.Errorf("Helpers line step %q, want %q", step, "Handing off")
	}
}
