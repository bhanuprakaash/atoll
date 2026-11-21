package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"atoll/store"
)

// gitBlocked puts a git on PATH whose rev-list (the read of commits ahead) waits until release is
// called, and returns a channel that closes when it has started. Other git commands run as usual.
func gitBlocked(t *testing.T) (started <-chan struct{}, release func()) {
	t.Helper()
	dir := t.TempDir()
	startedFile, releaseFile := filepath.Join(dir, "started"), filepath.Join(dir, "release")
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$*" in *rev-list*) ;; *) exec ` + gitBin + ` "$@" ;; esac
touch ` + startedFile + `
while [ ! -e ` + releaseFile + ` ]; do sleep 0.02; done
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ch := make(chan struct{})
	go func() {
		for {
			if _, err := os.Stat(startedFile); err == nil {
				close(ch)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return ch, func() { _ = os.WriteFile(releaseFile, nil, 0o644) }
}

// helperWithBranch is a helper thread with a branch and a worktree under a boss thread that has a
// branch, in a registered repo: the wake of its run reads git.
func helperWithBranch(t *testing.T, e *env) store.Thread {
	t.Helper()
	ctx := t.Context()
	dir, wt := t.TempDir(), filepath.Join(t.TempDir(), "helper")
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"},
		{"worktree", "add", "-q", "-b", "helper", wt},
	} {
		if out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	rp, err := e.st.CreateRepo(ctx, "repo", dir)
	if err != nil {
		t.Fatal(err)
	}
	boss := bossThread(t, e)
	helper, err := e.st.CreateHelperThread(ctx, boss.ID, e.thread.BotID, rp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetWorktree(ctx, boss.ID, "boss", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetWorktree(ctx, helper.ID, "helper", wt); err != nil {
		t.Fatal(err)
	}
	return helper
}

// writeNotBlocked checks that a write to the store succeeds well within busy_timeout while git is
// stuck, which it cannot if the wake runs git inside its write transaction.
func writeNotBlocked(t *testing.T, e *env, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("git did not start")
	}
	begin := time.Now()
	if _, err := e.st.CreateThread(t.Context(), e.thread.BotID, ""); err != nil {
		t.Fatalf("write while git runs: %v", err)
	}
	if took := time.Since(begin); took > time.Second {
		t.Errorf("write took %v while git runs, want it not to wait for git", took)
	}
}

func TestFailUnfinished_GitOutsideTransaction(t *testing.T) {
	e := newEnv(t, &fakeSession{})
	helper := helperWithBranch(t, e)
	if _, _, err := e.st.StartRunFrom(t.Context(), store.InboxItem{ThreadID: helper.ID, Text: "go"}); err != nil {
		t.Fatal(err)
	}
	started, release := gitBlocked(t)
	t.Cleanup(release)
	done := make(chan error, 1)
	go func() {
		_, err := FailUnfinished(context.Background(), e.st, "restart")
		done <- err
	}()
	writeNotBlocked(t, e, started)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFinish_GitOutsideTransaction(t *testing.T) {
	e := newEnv(t, &fakeSession{})
	e.m.cfg.IdleLimit = 50 * time.Millisecond
	helper := helperWithBranch(t, e)
	started, release := gitBlocked(t)
	t.Cleanup(release)
	if _, _, err := e.m.Send(t.Context(), helper.ID, "go", nil); err != nil {
		t.Fatal(err)
	}
	writeNotBlocked(t, e, started)
	release()
}
