package e2e

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A thread whose worktree folder was deleted: the next run fails with an error that names the
// folder and gives a next step. Go reports a missing working directory like a missing binary
// ("fork/exec <claude>: no such file or directory"), which sent the person looking for claude.
func TestRepo_DeletedWorktreeFailsInWords(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			connection, _ := f.connect(t)
			b := f.bot(t, p, "all", []string{connection})
			th, path, ch, _ := f.work(t, b, _fixRepoPrompt, "fix/seeded-bug")
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}

			r := f.s.send(t, th, _resumeRepoPrompt)
			done := f.s.finished(t, ch, r.ID)

			if done.Status != "failed" {
				t.Fatalf("run in a thread whose worktree is gone: status %q, error %q; want failed", done.Status, done.Error)
			}
			if !strings.Contains(done.Error, path) {
				t.Errorf("run error %q does not name the missing folder %q", done.Error, path)
			}
			for _, raw := range []string{"no such file or directory", "fork/exec", "start " + p} {
				if strings.Contains(strings.ToLower(done.Error), raw) {
					t.Errorf("run error %q holds %q: it reads as a missing %s binary", done.Error, raw, p)
				}
			}
			if !regexp.MustCompile(`(?i)new thread|restore|recreate|put it back|close this thread`).MatchString(done.Error) {
				t.Errorf("run error %q gives no next step (start a new thread, or restore the folder)", done.Error)
			}
		})
	}
}
