package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranch(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		slug    string
		want    string
		wantErr bool
	}{
		{name: "lowercases and dashes", typ: "fix", slug: "Seeded BUG", want: "fix/seeded-bug"},
		{name: "punctuation runs", typ: "feat", slug: "add  login!!/oauth", want: "feat/add-login-oauth"},
		{name: "junk at the ends", typ: "docs", slug: "  --Hello, world.--  ", want: "docs/hello-world"},
		{
			name: "cut to 40 without a trailing dash", typ: "chore",
			slug: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa b cccc",
			want: "chore/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{name: "empty slug", typ: "fix", slug: " !? ", wantErr: true},
		{name: "bad type", typ: "wip", slug: "x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Branch(tt.typ, tt.slug)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("Branch(%q, %q) = %q, %v; want %q, error %v", tt.typ, tt.slug, got, err, tt.want, tt.wantErr)
			}
		})
	}
	if _, err := Branch("wip", "x"); !errors.Is(err, ErrType) {
		t.Errorf("Branch with a bad type: %v, want ErrType", err)
	}
}

func TestWorktrees(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	src := t.TempDir()
	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(dir, name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(src, "init", "-q", "-b", "main")
	write(src, "a.txt", "one\n")
	git(src, "add", ".")
	git(src, "commit", "-q", "-m", "init")
	git(src, "remote", "add", "origin", src)
	write(src, ".git/config.lock", "")
	if _, _, _, err := Create(ctx, src, root, "demo", "fix/cleanup", ""); err == nil {
		t.Fatal("Create with locked config succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "fix-cleanup")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree after failed Create: %v", err)
	}
	if err := exec.Command("git", "-C", src, "show-ref", "--verify", "--quiet", "refs/heads/fix/cleanup").Run(); err == nil {
		t.Fatal("branch remains after failed Create")
	}
	if err := os.Remove(filepath.Join(src, ".git", "config.lock")); err != nil {
		t.Fatal(err)
	}
	git(src, "remote", "remove", "origin")

	if !IsRepo(ctx, src) || IsRepo(ctx, t.TempDir()) {
		t.Fatal("IsRepo: want true for the repo and false for a plain folder")
	}
	t.Run("snapshot ignores redirected common directory", func(t *testing.T) {
		work, _, _, err := Create(ctx, src, root, "demo", "fix/redirect", "")
		if err != nil {
			t.Fatal(err)
		}
		common := filepath.Join(src, ".git")
		id, err := WorktreeID(common, work)
		if err != nil {
			t.Fatal(err)
		}
		evil := t.TempDir()
		git(evil, "init", "-q")
		git(evil, "fetch", "-q", src, "HEAD:refs/heads/fix/redirect")
		write(work, "a.txt", "snapshot\n")
		commondir := filepath.Join(common, "worktrees", id, "commondir")
		defer func() { _ = os.WriteFile(commondir, []byte("../..\n"), 0o600) }()
		if err := os.WriteFile(commondir, []byte(filepath.Join(evil, ".git")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if saved, err := Save(WithWorktree(ctx, common, id), work, "refs/atoll/unfinished/redirect"); err != nil || !saved {
			t.Fatalf("Save = %v, %v", saved, err)
		}
		if got := git(src, "show", "refs/atoll/unfinished/redirect:a.txt"); got != "snapshot" {
			t.Fatalf("snapshot = %q", got)
		}
	})

	first, used, base, err := Create(ctx, src, root, "demo", "fix/bug", "")
	if err != nil || used != "fix/bug" || first != filepath.Join(root, "demo", "fix-bug") {
		t.Fatalf("first Create = %q, %q, %v", first, used, err)
	}
	initSHA := git(src, "rev-parse", "--short=7", "HEAD")
	if want := (Base{Name: "HEAD " + initSHA}); base != want {
		t.Errorf("base without origin = %+v, want %+v", base, want)
	}
	second, used, _, err := Create(ctx, src, root, "demo", "fix/bug", "")
	if err != nil || used != "fix/bug-2" || second != filepath.Join(root, "demo", "fix-bug-2") {
		t.Fatalf("second Create = %q, %q, %v", second, used, err)
	}

	const ref = "refs/atoll/unfinished/t1"
	if saved, err := Save(ctx, first, ref); err != nil || saved {
		t.Fatalf("Save of a clean checkout = %v, %v; want false, nil", saved, err)
	}
	write(first, "a.txt", "two\n")
	write(first, "new.txt", "fresh\n")
	if saved, err := Save(ctx, first, ref); err != nil || !saved {
		t.Fatalf("Save = %v, %v; want true, nil", saved, err)
	}
	for file, want := range map[string]string{"a.txt": "two", "new.txt": "fresh"} {
		if got := git(src, "show", ref+":"+file); got != want {
			t.Errorf("%s in saved ref = %q, want %q", file, got, want)
		}
	}
	if got := git(first, "status", "--porcelain"); got != "M a.txt\n?? new.txt" {
		t.Errorf("status after Save = %q, want the edits left in place", got)
	}

	if err := Remove(ctx, src, first); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree folder after Remove: %v, want not exist", err)
	}
	git(src, "rev-parse", "--verify", "refs/heads/fix/bug")
	if err := Remove(ctx, src, first); err != nil {
		t.Errorf("Remove of a missing dir: %v", err)
	}

	git(src, "update-ref", "refs/remotes/origin/main", "HEAD")
	git(src, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	_, _, base, err = Create(ctx, src, root, "demo", "feat/even", "")
	if want := (Base{Name: "origin/HEAD " + initSHA}); err != nil || base != want {
		t.Errorf("base level with origin = %+v, %v, want %+v", base, err, want)
	}
	for i, noun := range []string{"commit", "commits"} {
		write(src, "a.txt", noun)
		git(src, "commit", "-q", "-am", noun)
		tip := git(src, "rev-parse", "--short=7", "main")
		_, _, base, err = Create(ctx, src, root, "demo", "feat/ahead", "")
		want := Base{
			Name: "origin/HEAD " + initSHA,
			Note: fmt.Sprintf("local main is %d %s ahead (%s); not included", i+1, noun, tip),
		}
		if err != nil || base != want {
			t.Errorf("base with local ahead = %+v, %v, want %+v", base, err, want)
		}
	}

	dir := filepath.Join(root, "threads", "t2")
	for range 2 {
		if err := Checkout(ctx, src, dir); err != nil {
			t.Fatalf("Checkout: %v", err)
		}
	}
	if got := git(dir, "branch", "--show-current"); got != "" {
		t.Errorf("Checkout branch = %q, want detached", got)
	}
}

func TestSave_RepoFilters(t *testing.T) {
	for _, mode := range []string{"required", "include"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
			t.Setenv("GIT_AUTHOR_NAME", "t")
			t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
			t.Setenv("GIT_COMMITTER_NAME", "t")
			t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
			src, root, marker := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "ran")
			git := func(args ...string) {
				t.Helper()
				if out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", src}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			git("init", "-q")
			if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("before\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			git("add", "a.txt")
			git("commit", "-qm", "init")
			work, _, _, err := Create(t.Context(), src, root, "demo", "fix/filter", "")
			if err != nil {
				t.Fatal(err)
			}
			common := filepath.Join(src, ".git")
			id, err := WorktreeID(common, work)
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(common, "config")
			if mode == "include" {
				config = filepath.Join(common, "included")
				git("config", "include.path", config)
			}
			git("config", "--file", config, "filter.lreq.clean", "touch "+marker+"; cat")
			if mode == "required" {
				git("config", "--file", config, "filter.lreq.required", "true")
			}
			if err := os.WriteFile(filepath.Join(work, ".gitattributes"), []byte("a.txt filter=lreq\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("after\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if saved, err := Save(WithWorktree(t.Context(), common, id), work, "refs/atoll/unfinished/test"); err != nil || !saved {
				t.Fatalf("Save = %v, %v", saved, err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("filter ran: %v", err)
			}
		})
	}
}
