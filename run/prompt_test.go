package run

import (
	"strings"
	"testing"

	"atoll/memory"
	"atoll/store"
)

func TestMessagePrompt(t *testing.T) {
	tests := []struct {
		name   string
		items  []memory.Item
		thread store.Thread
		repo   store.Repo
		want   string
	}{
		{name: "first no repo", want: "Memory: repo 0, user 0, bot 0. Use recall.\n\nMessage:\nhello"},
		{name: "resumed no items", thread: store.Thread{Session: "s"}, want: "Message:\nhello"},
		{name: "first repo", items: []memory.Item{{ID: "a", Kind: "fact", Scope: "repo", Body: "body", Why: "why"}}, repo: store.Repo{ID: "r", Name: "repo"}, want: "Memory items:\n- [a] fact (repo): body\n  Why: why\n\nMemory: repo 0, user 0, bot 0. Use recall.\n\nRepo: repo (r)\nWorktree: /cwd\nBranch: detached\n\nMessage:\nhello"},
		{name: "cut body", thread: store.Thread{Session: "s"}, items: []memory.Item{{ID: "a", Kind: "fact", Scope: "user", Body: strings.Repeat("x", 401)}}, want: "Memory items:\n- [a] fact (user): " + strings.Repeat("x", 400) + "…\n\nMessage:\nhello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messagePrompt("hello", tt.items, memory.Counts{}, tt.thread, tt.repo, "/cwd"); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
