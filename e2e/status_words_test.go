package e2e

import (
	"strings"
	"testing"
)

func TestRun_StatusWords(t *testing.T) {
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
}
