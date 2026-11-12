package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepo_PlantedFilterDoesNotRunButUserFilterDoes plants a smudge filter in the repo's own config
// (what a Claude Bot on Auto can write), then makes Atoll check out a worktree twice: the thread's
// clean checkout and start_work. The user's own filter, defined in their global git config like
// Git LFS, must run on both; the planted one must not. A control runs plain git over the same
// repo and expects the planted filter to run, so a plant that stopped working cannot pass the test.
func TestRepo_PlantedFilterDoesNotRunButUserFilterDoes(t *testing.T) {
	for _, plant := range []struct {
		name string
		// config names the filter the planted config redefines; attributes is the planted
		// .git/info/attributes.
		config, attributes string
	}{
		{name: "new-filter", config: "evil", attributes: "answer.txt filter=evil\n"},
		{name: "overrides-user-filter", config: "user", attributes: "answer.txt filter=user\n"},
	} {
		t.Run(plant.name, func(t *testing.T) {
			f := newRepoFixture(t, "claude")
			evil := filepath.Join(t.TempDir(), "evil-ran")
			user := filepath.Join(t.TempDir(), "user-ran")
			scripts := t.TempDir()
			record := func(name, marker string) string {
				path := filepath.Join(scripts, name)
				writeExec(t, path, "#!/bin/sh\n[ \"$GIT_AUTHOR_NAME\" != \"Fixture Bot\" ] && echo \"$0 $PWD\" >> "+marker+"\ncat\n")
				return path
			}

			writeFile(t, filepath.Join(f.repo.Path, ".gitattributes"), "user.dat filter=user\n")
			writeFile(t, filepath.Join(f.repo.Path, "user.dat"), "content\n")
			gitIn(t, f.repo.Path, "add", ".gitattributes", "user.dat")
			gitIn(t, f.repo.Path, "commit", "-m", "add a user-filtered file")
			gitIn(t, f.repo.Path, "push", "origin", "main")
			writeFile(t, filepath.Join(f.m.home, ".gitconfig"), "[filter \"user\"]\n\tclean = cat\n\tsmudge = "+record("user-smudge", user)+"\n\trequired = true\n")

			gitIn(t, f.repo.Path, "config", "filter."+plant.config+".smudge", record("evil-smudge", evil))
			writeFile(t, filepath.Join(f.repo.Path, ".git", "info", "attributes"), plant.attributes)

			control := filepath.Join(t.TempDir(), "control")
			cmd := exec.CommandContext(t.Context(), "git", "-C", f.repo.Path, "worktree", "add", "--detach", control, "HEAD")
			cmd.Env = append(os.Environ(), "HOME="+f.m.home, "GIT_CONFIG_NOSYSTEM=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("plain git worktree add: %v: %s", err, out)
			}
			if _, err := os.ReadFile(evil); err != nil {
				t.Fatalf("plain git did not run the planted filter: the test would pass without testing anything")
			}
			gitIn(t, f.repo.Path, "worktree", "remove", "--force", control)
			for _, marker := range []string{evil, user} {
				if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}

			b := f.bot(t, "claude", "all", nil)
			th := f.thread(t, b)
			ch := f.s.events(t, th)
			for _, step := range []struct{ git, prompt string }{
				{"the thread's clean checkout", _questionRepoPrompt},
				{"start_work", _fixRepoPrompt},
			} {
				r := f.s.send(t, th, step.prompt)
				until(t, ch, r.ID, "run_finished")
				if done := f.s.run(t, r.ID); done.Status != "done" {
					t.Fatalf("run %q: %+v", step.prompt, done)
				}
				if ran, err := os.ReadFile(evil); err == nil {
					t.Errorf("%s ran the filter the Bot planted in the repo config:\n%s", step.git, ran)
				}
				if ran, err := os.ReadFile(user); err != nil || !strings.Contains(string(ran), "user-smudge") {
					t.Errorf("%s did not run the user's own filter: %q, %v", step.git, ran, err)
				}
				for _, marker := range []string{evil, user} {
					if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
