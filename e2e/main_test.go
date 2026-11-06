// Package e2e drives the real atoll binary the way a user would.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// _atoll is the path to the binary TestMain builds once for every test; _fakes is the folder
// with the fake claude and codex it builds next to it.
var _atoll, _fakes string

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
	for _, name := range []string{"claude", "codex", "glab", "gh"} {
		build := exec.Command("go", "build", "-o", filepath.Join(_fakes, name),
			"-ldflags", "-X atoll/e2e/fakes/fake._testdata="+testdata, "atoll/e2e/fakes/"+name)
		build.Stdout, build.Stderr = os.Stderr, os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "build fake %s: %v\n", name, err)
			return 1
		}
	}
	return m.Run()
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
