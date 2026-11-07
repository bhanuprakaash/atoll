// Package e2e drives the real atoll binary the way a user would.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// _atoll is the path to the binary TestMain builds once for every test; _fakes is the folder
// with the fake claude and codex it builds next to it. _keychains, on macOS, is the folder of the
// run's throwaway keychain.
var _atoll, _fakes, _keychains string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "atoll-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	_atoll = filepath.Join(dir, "atoll")
	build := exec.Command("go", "build", "-o", _atoll, "atoll/cmd/atoll")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build atoll:", err)
		return 1
	}

	testdata, err := filepath.Abs("testdata")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_fakes = filepath.Join(dir, "fakes")
	for _, name := range []string{"claude", "codex", "glab"} {
		build := exec.Command("go", "build", "-o", filepath.Join(_fakes, name),
			"-ldflags", "-X atoll/e2e/fakes/fake._testdata="+testdata, "atoll/e2e/fakes/"+name)
		build.Stdout, build.Stderr = os.Stderr, os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "build fake %s: %v\n", name, err)
			return 1
		}
	}
	if runtime.GOOS == "darwin" {
		// atoll and the tests' security calls use the keychains under $HOME. A temp HOME without
		// one makes macOS ask the user; the user's own login keychain must stay out of the run.
		home := filepath.Join(dir, "home")
		if err := newKeychain(home); err != nil {
			fmt.Fprintln(os.Stderr, "keychain:", err)
			return 1
		}
		_keychains = filepath.Join(home, "Library", "Keychains")
		_ = os.Setenv("HOME", home)
	}
	return m.Run()
}

// newKeychain creates home's unlocked login keychain, which never locks itself.
func newKeychain(home string) error {
	for _, args := range [][]string{
		{"create-keychain", "-p", "atoll-e2e", "login.keychain"},
		{"set-keychain-settings", filepath.Join(home, "Library", "Keychains", "login.keychain-db")},
	} {
		cmd := exec.Command("/usr/bin/security", args...)
		cmd.Env = []string{"HOME=" + home}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("security %v: %v: %s", args, err, out)
		}
	}
	return nil
}

func TestAtoll_PrintsName(t *testing.T) {
	cmd := exec.Command(_atoll)
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("atoll: %v\n%s", err, out)
	}
	if got, want := string(out), "atoll\n"; got != want {
		t.Errorf("atoll printed %q, want %q", got, want)
	}
}
