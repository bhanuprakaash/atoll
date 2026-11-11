package e2e

import (
	"path/filepath"
	"testing"
)

// A malformed .gitattributes line makes git warn on stderr. The warning is not a hash, so closing
// the thread must still save the work and remove the worktree.
func TestRepo_CloseWithBadGitattributes(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			b := f.bot(t, provider, "all", nil)
			th, path, ch, runID := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")
			writeFile(t, filepath.Join(path, ".gitattributes"), "a.txt \"sp ace\"\n")
			f.s.do(t, "POST", "/api/threads/"+th.ID+"/close", nil, nil, 204)
			e := assertRemoved(t, ch, runID, path, "closed")
			ref := "refs/atoll/unfinished/" + th.ID
			if e.Data["snapshot_ref"] != ref {
				t.Fatalf("snapshot event: %v", e.Data)
			}
			if got := gitIn(t, f.repo.Path, "cat-file", "-t", ref); got != "commit" {
				t.Fatalf("%s is a %q, want a commit", ref, got)
			}
			for name, want := range map[string]string{"answer.txt": "42", "unfinished.txt": "keep this untracked work"} {
				if got := gitIn(t, f.repo.Path, "show", ref+":"+name); got != want {
					t.Fatalf("saved %s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
