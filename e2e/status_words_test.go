package e2e

import (
	"strings"
	"testing"
)

func TestRun_StatusWords(t *testing.T) {
	_, s := start(t, "claude")
	th := s.newThread(t, chief(t, s).bot)
	ev := s.events(t, th)
	r := s.send(t, th, "Show status words for commands and platform tools.")
	es := until(t, ev, r.ID, "run_finished")
	var got, names []string
	for _, e := range es {
		if e.Type != "tool_call" {
			continue
		}
		name, _ := e.Data["name"].(string)
		names = append(names, name)
		verb, _ := e.Data["verb"].(string)
		target, _ := e.Data["target"].(string)
		got = append(got, strings.TrimSpace(verb+" "+target))
	}
	want := []string{"Running make check", "Waiting on a background task", "Waiting on a background task", "Looking around", "Looking around", "Setting up Example"}
	if len(got) != len(want)+1 {
		t.Fatalf("tool activity %q, want %d events", got, len(want)+1)
	}
	wantNames := []string{"Bash", "Bash", "Bash", "mcp__atoll__list_bots", "mcp__atoll__list_skills", "mcp__atoll__create_bot", "mcp__atoll__finish_run"}
	for i, name := range wantNames {
		if names[i] != name {
			t.Errorf("tool name %d = %q, want %q", i, names[i], name)
		}
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("tool activity %d = %q, want %q", i, got[i], w)
		}
	}
	if got[len(want)] == "Setting up" {
		t.Errorf("finish_run activity = %q", got[len(want)])
	}
}
