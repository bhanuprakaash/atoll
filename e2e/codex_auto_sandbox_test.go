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
			for _, prompt := range []string{_fixRepoPrompt, _resumeRepoPrompt} {
				r := f.s.send(t, th, prompt)
				if done := f.s.finished(t, ch, r.ID); done.Status != "done" {
					t.Fatalf("codex %s run %q: %+v", mode, prompt, done)
				}
			}

			sessions := f.m.sessions(t, "codex")
			if len(sessions) != 2 {
				t.Fatalf("codex ran %d sessions, want 2", len(sessions))
			}
			wantRoots := []string{f.root, filepath.Join(f.repo.Path, ".git")}
			for i, c := range sessions {
				checkMode(t, "codex", mode, c)
				method, roots, network := codexWorkspaceWrite(t, c)
				if want := []string{"thread/start", "thread/resume"}[i]; method != want {
					t.Fatalf("session %d began with %q, want %s", i+1, method, want)
				}
				if mode == "auto" {
					if !network {
						t.Errorf("codex on auto, %s: config.sandbox_workspace_write.network_access is not true", method)
					}
					if !slices.Equal(roots, wantRoots) {
						t.Errorf("codex on auto, %s: config.sandbox_workspace_write.writable_roots %q, want %q", method, roots, wantRoots)
					}
				} else if network || roots != nil {
					t.Errorf("codex on %s, %s: sandbox_workspace_write roots %q, network %v; want none", mode, method, roots, network)
				}
			}
		})
	}
}

// codexWorkspaceWrite is the method of a codex session's thread/start or thread/resume, and the
// writable_roots and network_access under its config.sandbox_workspace_write, the only place
// Codex reads them from at that point.
func codexWorkspaceWrite(t *testing.T, c *call) (method string, roots []string, network bool) {
	t.Helper()
	for _, line := range c.stdin {
		var r struct {
			Method string `json:"method"`
			Params struct {
				Config struct {
					SandboxWorkspaceWrite struct {
						WritableRoots []string `json:"writable_roots"`
						NetworkAccess bool     `json:"network_access"`
					} `json:"sandbox_workspace_write"`
				} `json:"config"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &r) == nil && (r.Method == "thread/start" || r.Method == "thread/resume") {
			w := r.Params.Config.SandboxWorkspaceWrite
			return r.Method, w.WritableRoots, w.NetworkAccess
		}
	}
	t.Fatal("codex session has no thread/start or thread/resume")
	return "", nil, false
}
