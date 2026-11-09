package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: start-work-base-contract.md.

type baseCase struct{ provider, mode string }

var _baseCases = []baseCase{{"claude", ""}, {"codex", ""}, {"codex", "auto"}}

func (c baseCase) name() string { return strings.Trim(c.provider+"/"+c.mode, "/") }

// pushBase puts a branch on origin with a commit the default branch lacks, fetches it into the
// repo (the server never fetches) and returns the commit's short id.
func (f *repoFixture) pushBase(t *testing.T, name string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, t.TempDir(), "clone", f.origin, clone)
	gitIn(t, clone, "switch", "-c", name)
	writeFile(t, filepath.Join(clone, "base-only.txt"), "only on the base\n")
	gitIn(t, clone, "add", "base-only.txt")
	gitIn(t, clone, "commit", "-m", "test: base only")
	gitIn(t, clone, "push", "origin", name)
	gitIn(t, f.repo.Path, "fetch", "origin")
	return gitIn(t, f.repo.Path, "rev-parse", "--short=7", "origin/"+name)
}

func (f *repoFixture) baseThread(t *testing.T, c baseCase) (thread, <-chan event) {
	t.Helper()
	body := map[string]any{"name": "Repo Bot", "persona": "Fix carefully.", "provider": c.provider, "repo_access": "all"}
	if c.mode != "" {
		body["mode"] = c.mode
	}
	var b bot
	f.s.do(t, "POST", "/api/bots", body, &b, 201)
	th := f.thread(t, b)
	return th, f.s.events(t, th)
}

func startWorkCall(args string, schema bool) fake.MCPCall {
	return fake.MCPCall{Server: "atoll", Tool: "start_work", Args: json.RawMessage(args), Schema: schema}
}

// startWorkResult is the reply line of a start_work call: its JSON result, or "" when it failed.
func startWorkResult(t *testing.T, line string) (res struct {
	Path, Branch, Base string
}, errText string) {
	t.Helper()
	text, ok := strings.CutPrefix(line, "atoll/start_work: ")
	if !ok {
		t.Fatalf("reply line %q is not a start_work result", line)
	}
	var raw struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
		Base   string `json:"base"`
	}
	if json.Unmarshal([]byte(text), &raw) != nil || raw.Path == "" {
		return res, text
	}
	res.Path, res.Branch, res.Base = raw.Path, raw.Branch, raw.Base
	return res, ""
}

// TestRepo_StartWorkBranchesFromBase: with base, the new branch starts at origin/<base>, Reef sets
// its upstream, and the reply names the base and its short commit. On Codex Auto the worktree is
// the one the sandbox roots are made for; the fake enforces no sandbox, so the commit and push
// below run unsandboxed and only prove the branch is pushable.
func TestRepo_StartWorkBranchesFromBase(t *testing.T) {
	for _, c := range _baseCases {
		t.Run(c.name(), func(t *testing.T) {
			f := newRepoFixture(t, c.provider)
			short := f.pushBase(t, "test/x")
			th, ev := f.baseThread(t, c)
			got, bad := startWorkResult(t, useAtoll(t, f.s, th, ev, startWorkCall(`{"type":"fix","slug":"Seeded BUG","base":"test/x"}`, false))[0])
			if bad != "" {
				t.Fatalf("start_work with base test/x failed: %s", bad)
			}
			if got.Branch != "fix/seeded-bug" {
				t.Errorf("branch %q, want fix/seeded-bug", got.Branch)
			}
			if !strings.Contains(got.Base, "test/x") || !strings.Contains(got.Base, short) {
				t.Errorf("reply base %q, want it to name test/x and %s", got.Base, short)
			}
			underRoot(t, resolved(t, f.root)[0], resolved(t, got.Path)[0])
			if head := gitIn(t, got.Path, "rev-parse", "--short=7", "HEAD"); head != short {
				t.Errorf("worktree starts at %s, want origin/test/x %s", head, short)
			}
			if _, err := os.Stat(filepath.Join(got.Path, "base-only.txt")); err != nil {
				t.Errorf("worktree lacks the base's file: %v", err)
			}
			for key, want := range map[string]string{
				"branch.fix/seeded-bug.remote": "origin",
				"branch.fix/seeded-bug.merge":  "refs/heads/fix/seeded-bug",
			} {
				if v := gitIn(t, got.Path, "config", "--default", "", "--get", key); v != want {
					t.Errorf("git config %s in the worktree = %q, want %q", key, v, want)
				}
			}

			writeFile(t, filepath.Join(got.Path, "answer.txt"), "42\n")
			gitIn(t, got.Path, "add", "answer.txt")
			gitIn(t, got.Path, "commit", "-m", "fix: correct seeded answer")
			gitIn(t, got.Path, "push", "origin", "HEAD:refs/heads/fix/seeded-bug")
			if out := gitIn(t, f.repo.Path, "--git-dir", f.origin, "show", "fix/seeded-bug:base-only.txt"); out != "only on the base" {
				t.Errorf("pushed branch lacks the base's commit: %q", out)
			}
		})
	}
}

// TestRepo_StartWorkRefusesBadBase: a base that is not a branch on origin, or not a plain branch
// name, is a tool error that names the valid form, and makes no worktree or branch.
func TestRepo_StartWorkRefusesBadBase(t *testing.T) {
	for _, c := range _baseCases {
		t.Run(c.name(), func(t *testing.T) {
			f := newRepoFixture(t, c.provider)
			f.pushBase(t, "test/x")
			full := gitIn(t, f.repo.Path, "rev-parse", "origin/test/x")
			bases := []string{"test/missing", "origin/test/x", full, "test/x..main", "-x", "test/x y"}
			var calls []fake.MCPCall
			for _, b := range bases {
				arg, _ := json.Marshal(map[string]string{"type": "fix", "slug": "Seeded BUG", "base": b})
				calls = append(calls, startWorkCall(string(arg), false))
			}
			th, ev := f.baseThread(t, c)
			lines := useAtoll(t, f.s, th, ev, calls...)
			for i, b := range bases {
				res, bad := startWorkResult(t, lines[i])
				if bad == "" {
					t.Errorf("start_work with base %q made %+v, want a refusal", b, res)
					continue
				}
				low := strings.ToLower(bad)
				for _, word := range []string{"base", "branch", "origin", "test/x"} {
					if !strings.Contains(low, word) {
						t.Errorf("refusal of base %q is %q, want it to name the valid form (a branch on origin, e.g. test/x): no %q", b, bad, word)
					}
				}
			}
			if p := worktreeOf(t, f.repo.Path, "fix/seeded-bug"); p != "" {
				t.Errorf("a refused start_work left a worktree at %s", p)
			}
			if out := gitIn(t, f.repo.Path, "branch", "--list", "fix/*"); out != "" {
				t.Errorf("a refused start_work left branches: %q", out)
			}
			if entries, _ := os.ReadDir(f.root); len(entries) != 0 {
				t.Errorf("a refused start_work left %d entries under the worktree root", len(entries))
			}
		})
	}
}

// TestRepo_StartWorkDeclaresBase: the start_work schema the Bot reads declares base, so a Bot
// sending only declared arguments gets its worktree on the base.
func TestRepo_StartWorkDeclaresBase(t *testing.T) {
	for _, c := range _baseCases[:2] {
		t.Run(c.name(), func(t *testing.T) {
			f := newRepoFixture(t, c.provider)
			f.pushBase(t, "test/x")
			th, ev := f.baseThread(t, c)
			line := useAtoll(t, f.s, th, ev, startWorkCall(`{"type":"fix","slug":"Seeded BUG","base":"test/x"}`, true))[0]
			if _, bad := startWorkResult(t, line); bad != "" {
				t.Errorf("start_work with base: %s", bad)
			}
		})
	}
}
