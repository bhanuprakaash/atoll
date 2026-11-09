package e2e

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
)

func TestCodexAuto_SandboxCoversWorktreeAndNetwork(t *testing.T) {
	for _, mode := range _modes {
		t.Run(mode, func(t *testing.T) {
			f := newRepoFixture(t, "codex")
			var b bot
			f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Codex " + mode, "persona": "Fix carefully.", "provider": "codex", "mode": mode, "repo_access": "all"}, &b, 201)
			th := f.thread(t, b)
			ch := f.s.events(t, th)
			r := f.s.send(t, th, _fixRepoPrompt)
			es := until(t, ch, r.ID, "run_finished")
			if done := f.s.run(t, r.ID); done.Status != "done" {
				t.Fatalf("codex %s run: %+v", mode, done)
			}
			worktree, _ := repoEvent(t, es, "worktree_created").Data["path"].(string)

			sessions := f.m.sessions(t, "codex")
			if len(sessions) != 1 {
				t.Fatalf("codex ran %d sessions, want 1", len(sessions))
			}
			checkMode(t, "codex", mode, sessions[0])
			roots, network := codexSandbox(t, sessions[0])
			gitDir := filepath.Join(f.repo.Path, ".git")

			if mode == "auto" {
				for _, want := range []string{worktree, gitDir} {
					if !slices.Contains(roots, want) {
						t.Errorf("codex on auto: writable roots %q lack %q", roots, want)
					}
				}
				if !network {
					t.Error("codex on auto: workspace-write sandbox has no network access, want it on")
				}
				return
			}
			if mode == "ask" && (network || slices.Contains(roots, worktree) || slices.Contains(roots, gitDir)) {
				t.Errorf("codex on ask: writable roots %q, network %v; want neither the worktree, the repo's .git nor network", roots, network)
			}
		})
	}
}

// codexSandbox is every writable root and whether any network access was asked for in the
// messages a codex session got, as params or config, under any sandbox key spelling.
func codexSandbox(t *testing.T, c *call) (roots []string, network bool) {
	t.Helper()
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, x := range v {
				walk(x)
			}
		case map[string]any:
			for k, x := range v {
				switch k {
				case "writableRoots", "writable_roots":
					list, _ := x.([]any)
					for _, r := range list {
						s, _ := r.(string)
						roots = append(roots, s)
					}
				case "networkAccess", "network_access":
					network = network || x == true
				}
				walk(x)
			}
		}
	}
	for _, line := range c.stdin {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			walk(m)
		}
	}
	return roots, network
}
