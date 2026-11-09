package e2e

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
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
			common := gitIn(t, f.repo.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
			for i, c := range sessions {
				checkMode(t, "codex", mode, c)
				method, cwd, present, roots, network := codexWorkspaceWrite(t, c)
				if want := []string{"thread/start", "thread/resume"}[i]; method != want {
					t.Fatalf("session %d began with %q, want %s", i+1, method, want)
				}
				if mode == "auto" {
					wantRoots := resolved(t, f.root, common, gitIn(t, cwd, "rev-parse", "--absolute-git-dir"))
					if len(wantRoots) != 3 || i == 1 && !strings.HasPrefix(resolved(t, cwd)[0], resolved(t, f.root)[0]+string(filepath.Separator)) {
						t.Fatalf("%s ran in %q, want a linked worktree (with its own git dir), under %q when resumed", method, cwd, f.root)
					}
					if !network {
						t.Errorf("codex on auto, %s: config.sandbox_workspace_write.network_access is not true", method)
					}
					if got := resolved(t, roots...); !slices.Equal(got, wantRoots) {
						t.Errorf("codex on auto, %s: config.sandbox_workspace_write.writable_roots %q, want %q in any order (cwd %s)", method, got, wantRoots, cwd)
					}
				} else if present {
					t.Errorf("codex on %s, %s: config.sandbox_workspace_write is set; want it absent", mode, method)
				}
			}
		})
	}
}

// resolved is paths cleaned, with symlinks resolved, sorted and without repeats: macOS temp folders sit behind a
// symlink that git resolves.
func resolved(t *testing.T, paths ...string) []string {
	t.Helper()
	out := make([]string, len(paths))
	for i, p := range paths {
		var err error
		if out[i], err = filepath.EvalSymlinks(filepath.Clean(p)); err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// codexWorkspaceWrite is the method and cwd of a codex session's thread/start or thread/resume, whether
// its config has sandbox_workspace_write, and the writable_roots and network_access under it, the
// only place Codex reads them from at that point.
func codexWorkspaceWrite(t *testing.T, c *call) (method, cwd string, present bool, roots []string, network bool) {
	t.Helper()
	for _, line := range c.stdin {
		var r struct {
			Method string `json:"method"`
			Params struct {
				Cwd    string `json:"cwd"`
				Config struct {
					SandboxWorkspaceWrite *struct {
						WritableRoots []string `json:"writable_roots"`
						NetworkAccess bool     `json:"network_access"`
					} `json:"sandbox_workspace_write"`
				} `json:"config"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &r) == nil && (r.Method == "thread/start" || r.Method == "thread/resume") {
			w := r.Params.Config.SandboxWorkspaceWrite
			if w == nil {
				return r.Method, r.Params.Cwd, false, nil, false
			}
			return r.Method, r.Params.Cwd, true, w.WritableRoots, w.NetworkAccess
		}
	}
	t.Fatal("codex session has no thread/start or thread/resume")
	return "", "", false, nil, false
}
