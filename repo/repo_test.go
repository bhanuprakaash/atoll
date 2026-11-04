package repo

import (
	"context"
	"errors"
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

	if !IsRepo(ctx, src) || IsRepo(ctx, t.TempDir()) {
		t.Fatal("IsRepo: want true for the repo and false for a plain folder")
	}

	first, used, err := Create(ctx, src, root, "demo", "fix/bug")
	if err != nil || used != "fix/bug" || first != filepath.Join(root, "demo", "fix-bug") {
		t.Fatalf("first Create = %q, %q, %v", first, used, err)
	}
	second, used, err := Create(ctx, src, root, "demo", "fix/bug")
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
