package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Contract: docs/design.md, Repos and worktrees (the read-only checkout's base, the behind note).

// advanceOrigin pushes a commit that sets answer.txt to answer onto the bare origin and updates
// the registered clone's origin/main with git fetch. Its local main stays where it was. It
// returns the new origin/main commit.
func (f *repoFixture) advanceOrigin(t *testing.T, answer string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", f.origin, other)
	writeFile(t, filepath.Join(other, "answer.txt"), answer+"\n")
	gitIn(t, other, "commit", "-am", "origin moves to "+answer)
	gitIn(t, other, "push", "origin", "main")
	gitIn(t, f.repo.Path, "fetch", "origin")
	gitIn(t, f.repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return gitIn(t, f.repo.Path, "rev-parse", "origin/HEAD")
}

func (f *repoFixture) checkoutDir(th thread) string {
	return filepath.Join(f.m.data, "threads", th.ID)
}

// ask sends prompt, waits for the run and returns its reply and first message.
func (f *repoFixture) ask(t *testing.T, th thread, prompt string) (reply, message string) {
	t.Helper()
	ch := f.s.events(t, th)
	r := f.s.send(t, th, prompt)
	es := until(t, ch, r.ID, "run_finished")
	done := f.s.run(t, r.ID)
	if done.Status != "done" {
		t.Fatalf("run: %+v", done)
	}
	message, _ = repoEvent(t, es, "prompt").Data["message"].(string)
	return done.Reply, message
}

func TestRepo_ThreadCheckoutIsAtStartWorkBase(t *testing.T) {
	for _, provider := range _providers {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			base := f.advanceOrigin(t, "43")
			if local := gitIn(t, f.repo.Path, "rev-parse", "main"); local == base {
				t.Fatal("fixture: local main is not behind origin/main")
			}
			b := f.bot(t, provider, "all", nil)

			reader := f.thread(t, b)
			reply, _ := f.ask(t, reader, _questionRepoPrompt)
			if got := gitIn(t, f.checkoutDir(reader), "rev-parse", "HEAD"); got != base {
				t.Errorf("read-only checkout at %s, want origin/HEAD %s (local main is behind)", got, base)
			}
			if !strings.Contains(reply, ": 43") {
				t.Errorf("the thread read %q, want origin's answer 43", reply)
			}

			_, worktree, _, _ := f.work(t, b, _fixRepoPrompt, "fix/seeded-bug")
			if got := gitIn(t, worktree, "rev-parse", "HEAD^"); got != base {
				t.Fatalf("start_work branched from %s, want origin/HEAD %s", got, base)
			}
		})
	}
}

func TestRepo_ThreadSaysLocalBranchIsBehindOnce(t *testing.T) {
	for _, provider := range _providers {
		for _, c := range []struct {
			name    string
			commits int
			note    string
		}{
			{name: "level", commits: 0},
			{name: "one", commits: 1, note: "1 commit"},
			{name: "two", commits: 2, note: "2 commits"},
		} {
			t.Run(provider+"/"+c.name, func(t *testing.T) {
				f := newRepoFixture(t, provider)
				gitIn(t, f.repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
				var tip string
				for i := 0; i < c.commits; i++ {
					tip = f.advanceOrigin(t, fmt.Sprintf("%d", 50+i))
				}
				th := f.thread(t, f.bot(t, provider, "all", nil))

				_, first := f.ask(t, th, _questionRepoPrompt)
				short := gitIn(t, f.repo.Path, "rev-parse", "--short=7", "origin/HEAD")
				want := fmt.Sprintf("Note: local main is %s behind origin/HEAD (%s); the checkout is at origin/HEAD", c.note, short)
				if got := strings.Count(first, "behind"); c.commits == 0 && got != 0 {
					t.Errorf("first message says behind with local main level with origin:\n%s", first)
				} else if c.commits > 0 && (got != 1 || strings.Count(first, want) != 1) {
					t.Errorf("first message has the behind note %d times, want once as %q (origin tip %s):\n%s", got, want, tip, first)
				}

				_, later := f.ask(t, th, _resumeRepoPrompt)
				if strings.Contains(later, "behind") {
					t.Errorf("a later message repeats the behind note:\n%s", later)
				}
			})
		}
	}
}

func TestRepo_LongLivedThreadCheckoutFollowsBase(t *testing.T) {
	for _, provider := range _providers {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			gitIn(t, f.repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
			th := f.thread(t, f.bot(t, provider, "all", nil))
			first := gitIn(t, f.repo.Path, "rev-parse", "origin/HEAD")

			reply, _ := f.ask(t, th, _questionRepoPrompt)
			if got := gitIn(t, f.checkoutDir(th), "rev-parse", "HEAD"); got != first || !strings.Contains(reply, ": 41") {
				t.Fatalf("first run: checkout at %s with reply %q, want %s and answer 41", got, reply, first)
			}

			moved := f.advanceOrigin(t, "43")
			reply, _ = f.ask(t, th, _resumeRepoPrompt)
			if got := gitIn(t, f.checkoutDir(th), "rev-parse", "HEAD"); got != moved {
				t.Errorf("checkout at %s after origin moved, want the new base %s", got, moved)
			}
			if !strings.Contains(reply, ": 43") {
				t.Errorf("second run read %q, want the new answer 43", reply)
			}
		})
	}
}
