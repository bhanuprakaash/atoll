package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
				if v := gitIn(t, got.Path, "config", "--local", "--default", "", "--get", key); v != want {
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

// madeNothing takes what start_work makes (local branches, branch worktrees, entries under the
// worktree root) and returns a check that none was added since. The thread's own read-only
// checkout is a detached worktree outside the root, which a first run may make, so it is not
// counted.
func (f *repoFixture) madeNothing(t *testing.T) func() {
	t.Helper()
	snap := func() string {
		var out []string
		out = append(out, strings.Split(gitIn(t, f.repo.Path, "for-each-ref", "--format=branch %(refname)", "refs/heads"), "\n")...)
		path := ""
		for _, l := range strings.Split(gitIn(t, f.repo.Path, "worktree", "list", "--porcelain"), "\n") {
			if p, ok := strings.CutPrefix(l, "worktree "); ok {
				path = p
			}
			if strings.HasPrefix(l, "branch ") {
				out = append(out, "worktree "+path+" "+l)
			}
		}
		entries, _ := os.ReadDir(f.root)
		for _, e := range entries {
			out = append(out, "root "+e.Name())
		}
		slices.Sort(out)
		return strings.Join(out, "\n")
	}
	before := snap()
	return func() {
		t.Helper()
		if after := snap(); after != before {
			t.Errorf("a refused start_work changed the repo's branches, branch worktrees or the worktree root:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	}
}

// tryStartWork has the Bot call start_work once. A refusal reaches the Bot as a tool error line or
// as a failed call that fails the run; either way bad is the error text, and res is empty.
func tryStartWork(t *testing.T, f *repoFixture, th thread, ev <-chan event, call fake.MCPCall) (res struct{ Path, Branch, Base string }, bad string) {
	t.Helper()
	payload, _ := json.Marshal([]fake.MCPCall{call})
	r := f.s.finished(t, ev, f.s.send(t, th, _mcpPromptHead+string(payload)).ID)
	if r.Status != "done" {
		return res, r.Error
	}
	return startWorkResult(t, strings.Split(r.Reply, "\n")[0])
}

// refuses runs start_work with each base in its own subtest and expects a refusal that names the
// valid form by meaning (a branch on origin, with a <type>/<slug>-style example) and leaves no
// worktree, branch or folder.
func refuses(t *testing.T, c baseCase, f *repoFixture, base string) {
	t.Helper()
	arg, _ := json.Marshal(map[string]string{"type": "fix", "slug": "Seeded BUG", "base": base})
	th, ev := f.baseThread(t, c)
	unchanged := f.madeNothing(t)
	res, bad := tryStartWork(t, f, th, ev, startWorkCall(string(arg), false))
	if bad == "" {
		t.Errorf("start_work with base %q made %+v, want a refusal", base, res)
	} else {
		low := strings.ToLower(strings.ReplaceAll(bad, base, ""))
		if !strings.Contains(low, "origin") || !strings.Contains(low, "branch") || !regexp.MustCompile(`[\w<>-]+/[\w<>-]+`).MatchString(low) {
			t.Errorf("refusal of base %q is %q, want it to say a base is a branch on origin, with a <type>/<slug>-style example", base, bad)
		}
	}
	unchanged()
}

// TestRepo_StartWorkRefusesBadBase: a base that is not a branch on origin, or not a plain branch
// name, is a tool error that names the valid form, and makes no worktree and no branch.
func TestRepo_StartWorkRefusesBadBase(t *testing.T) {
	bases := []struct {
		name, base string
		setup      func(t *testing.T, f *repoFixture)
	}{
		{name: "unknown", base: "test/missing"},
		{name: "local-branch-not-on-origin", base: "local-only", setup: func(t *testing.T, f *repoFixture) {
			gitIn(t, f.repo.Path, "branch", "local-only", "main")
		}},
		{name: "tag-not-a-branch", base: "v1", setup: func(t *testing.T, f *repoFixture) {
			gitIn(t, f.repo.Path, "tag", "v1", "main")
		}},
		{name: "origin-prefixed", base: "origin/test/x"},
		{name: "full-sha", base: "SHA"},
		{name: "refs-heads", base: "refs/heads/test/x"},
		{name: "head", base: "HEAD"},
		{name: "at", base: "@"},
		{name: "dotdot", base: "test/x..main"},
		{name: "leading-dash", base: "-x"},
		{name: "space", base: "test/x y"},
		{name: "caret", base: "test/x^"},
		{name: "tilde", base: "test/x~1"},
		{name: "colon-path", base: "test/x:base-only.txt"},
		{name: "reflog", base: "test/x@{1}"},
		{name: "lock-suffix", base: "test/x.lock"},
		{name: "the-new-branch", base: "fix/seeded-bug", setup: func(t *testing.T, f *repoFixture) {
			f.pushBase(t, "fix/seeded-bug")
		}},
	}
	for _, c := range _baseCases[:2] {
		for _, b := range bases {
			t.Run(c.name()+"/"+b.name, func(t *testing.T) {
				f := newRepoFixture(t, c.provider)
				f.pushBase(t, "test/x")
				if b.setup != nil {
					b.setup(t, f)
				}
				base := b.base
				if base == "SHA" {
					base = gitIn(t, f.repo.Path, "rev-parse", "origin/test/x")
				}
				refuses(t, c, f, base)
			})
		}
	}
}

// TestRepo_StartWorkEmptyBaseIsNoBase: a Bot that fills the optional base with "" gets what it
// gets with no base: a branch from origin/HEAD, not a refusal.
func TestRepo_StartWorkEmptyBaseIsNoBase(t *testing.T) {
	for _, c := range _baseCases[:2] {
		t.Run(c.name(), func(t *testing.T) {
			f := newRepoFixture(t, c.provider)
			gitIn(t, f.repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
			f.pushBase(t, "test/x")
			short := gitIn(t, f.repo.Path, "rev-parse", "--short=7", "origin/HEAD")
			th, ev := f.baseThread(t, c)
			got, bad := startWorkResult(t, useAtoll(t, f.s, th, ev, startWorkCall(`{"type":"fix","slug":"Seeded BUG","base":""}`, false))[0])
			if bad != "" {
				t.Fatalf("start_work with an empty base failed: %s", bad)
			}
			if want := "origin/HEAD " + short; got.Branch != "fix/seeded-bug" || got.Base != want {
				t.Errorf("got branch %q, base %q; want fix/seeded-bug, %q", got.Branch, got.Base, want)
			}
			if head := gitIn(t, got.Path, "rev-parse", "--short=7", "HEAD"); head != short {
				t.Errorf("worktree starts at %s, want origin/HEAD %s", head, short)
			}
		})
	}
}

// TestRepo_StartWorkBaseBeatsTag: a tag named like the base branch does not stand in for it.
func TestRepo_StartWorkBaseBeatsTag(t *testing.T) {
	f := newRepoFixture(t, "claude")
	short := f.pushBase(t, "test/x")
	gitIn(t, f.repo.Path, "tag", "test/x", "main")
	th, ev := f.baseThread(t, baseCase{provider: "claude"})
	got, bad := startWorkResult(t, useAtoll(t, f.s, th, ev, startWorkCall(`{"type":"fix","slug":"Seeded BUG","base":"test/x"}`, false))[0])
	if bad != "" {
		t.Fatalf("start_work with base test/x failed: %s", bad)
	}
	if head := gitIn(t, got.Path, "rev-parse", "--short=7", "HEAD"); head != short {
		t.Errorf("worktree starts at %s, want origin/test/x %s, not the tag", head, short)
	}
}

// TestRepo_StartWorkDeclaresBase: the start_work schema the Bot reads declares base, so a Bot
// sending only declared arguments gets its worktree on the base. The schema does not depend on
// the mode, so Codex on Auto is not repeated.
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

// TestRepo_StartWorkTypes: type test makes test/<slug>, the branch of a test PR; any other type is
// refused naming the valid ones and makes nothing.
func TestRepo_StartWorkTypes(t *testing.T) {
	for _, c := range _baseCases[:2] {
		t.Run(c.name()+"/test", func(t *testing.T) {
			f := newRepoFixture(t, c.provider)
			th, ev := f.baseThread(t, c)
			got, bad := tryStartWork(t, f, th, ev, startWorkCall(`{"type":"test","slug":"x"}`, false))
			if bad != "" {
				t.Fatalf("start_work with type test failed: %s", bad)
			}
			if got.Branch != "test/x" || worktreeOf(t, f.repo.Path, "test/x") == "" {
				t.Errorf("got branch %q, worktree %q; want branch test/x with a worktree", got.Branch, worktreeOf(t, f.repo.Path, "test/x"))
			}
		})
		t.Run(c.name()+"/unknown", func(t *testing.T) {
			f := newRepoFixture(t, c.provider)
			th, ev := f.baseThread(t, c)
			unchanged := f.madeNothing(t)
			res, bad := tryStartWork(t, f, th, ev, startWorkCall(`{"type":"bogus","slug":"x"}`, false))
			if bad == "" {
				t.Fatalf("start_work with type bogus made %+v, want a refusal", res)
			}
			for _, typ := range []string{"feat", "fix", "chore", "docs", "test"} {
				if !strings.Contains(bad, typ) {
					t.Errorf("refusal %q does not name the valid type %q", bad, typ)
				}
			}
			unchanged()
		})
	}
}
