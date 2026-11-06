package e2e

import (
	"fmt"
	"strings"
	"testing"
)

func TestRepo_StartWorkReplyNamesOriginHeadBase(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			gitIn(t, f.repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
			base := gitIn(t, f.repo.Path, "rev-parse", "--short=7", "origin/HEAD")
			path, reply := f.startWorkReply(t, provider, _fixRepoPrompt)

			want := fmt.Sprintf(`{"path":%q,"branch":"fix/seeded-bug","base":"origin/HEAD %s"}`, path, base)
			assertStartWorkResult(t, reply, want)
		})
	}
}

func TestRepo_StartWorkReplyNotesLocalBranchAhead(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		for _, ahead := range []struct {
			name    string
			commits int
			note    string
		}{
			{name: "one", commits: 1, note: "1 commit"},
			{name: "two", commits: 2, note: "2 commits"},
		} {
			t.Run(provider+"/"+ahead.name, func(t *testing.T) {
				f := newRepoFixture(t, provider)
				gitIn(t, f.repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
				for i := 1; i <= ahead.commits; i++ {
					writeFile(t, fmt.Sprintf("%s/local-%d.txt", f.repo.Path, i), "local only\n")
					gitIn(t, f.repo.Path, "add", fmt.Sprintf("local-%d.txt", i))
					gitIn(t, f.repo.Path, "commit", "-m", fmt.Sprintf("local ahead %d", i))
				}
				base := gitIn(t, f.repo.Path, "rev-parse", "--short=7", "origin/HEAD")
				tip := gitIn(t, f.repo.Path, "rev-parse", "--short=7", "main")
				path, reply := f.startWorkReply(t, provider, _fixRepoPrompt)

				want := fmt.Sprintf(`{"path":%q,"branch":"fix/seeded-bug","base":"origin/HEAD %s","note":"local main is %s ahead (%s); not included"}`, path, base, ahead.note, tip)
				assertStartWorkResult(t, reply, want)
			})
		}
	}
}

func TestRepo_StartWorkReplyOmitsAheadNoteWithoutOrigin(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			gitIn(t, f.repo.Path, "remote", "remove", "origin")
			base := gitIn(t, f.repo.Path, "rev-parse", "--short=7", "HEAD")
			path, reply := f.startWorkReply(t, provider, _unfinishedRepoPrompt)

			want := fmt.Sprintf(`{"path":%q,"branch":"fix/seeded-bug","base":"HEAD %s"}`, path, base)
			assertStartWorkResult(t, reply, want)
		})
	}
}

func (f *repoFixture) startWorkReply(t *testing.T, provider, prompt string) (string, string) {
	t.Helper()
	b := f.bot(t, provider, "all", nil)
	th := f.thread(t, b)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, prompt)
	es := until(t, ch, r.ID, "run_finished")
	done := f.s.run(t, r.ID)
	if done.Status != "done" {
		t.Fatalf("repository run: %+v", done)
	}
	created := repoEvent(t, es, "worktree_created")
	path, _ := created.Data["path"].(string)
	if path == "" {
		t.Fatalf("worktree event: %v", created.Data)
	}
	return path, done.Reply
}

func assertStartWorkResult(t *testing.T, reply, want string) {
	t.Helper()
	got, ok := strings.CutPrefix(reply[strings.LastIndex(reply, "\n")+1:], "start_work result: ")
	if !ok || got != want {
		t.Fatalf("start_work result: got %q, want %q", got, want)
	}
}
