package fake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	_stackBase = regexp.MustCompile(`Stack a fix on (\S+)`)
)

// StackStep is a Bot stacking a fix on another PR's branch: the prompt `Stack a fix on <branch>`.
// It makes its worktree, builds on the branch, pushes, and opens the pull request with gh against
// that branch. It tracks the pull request and returns the reply.
func StackStep(c *MCP, prompt string) (string, error) {
	m := _stackBase.FindStringSubmatch(prompt)
	if m == nil || c == nil {
		return "", fmt.Errorf("stack step needs `Stack a fix on <branch>` and the atoll MCP server")
	}
	base := m[1]
	out, bad, err := c.Call("start_work", json.RawMessage(`{"type":"fix","slug":"Stacked fix"}`))
	if err != nil || bad {
		return "", fmt.Errorf("start_work: %v %v: %s", err, bad, out)
	}
	var work struct{ Path, Branch string }
	if err := json.Unmarshal([]byte(out), &work); err != nil || work.Path == "" {
		return "", fmt.Errorf("start_work: %w: %s", err, out)
	}
	for _, args := range [][]string{{"fetch", "origin", base}, {"reset", "--hard", "FETCH_HEAD"}} {
		if _, err := run(work.Path, "git", args...); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(work.Path, "stacked.txt"), []byte("built on "+base+"\n"), 0o644); err != nil {
		return "", err
	}
	for _, args := range [][]string{{"add", "stacked.txt"}, {"commit", "-m", "fix: stacked change"}, {"push", "-u", "origin", work.Branch}} {
		if _, err := run(work.Path, "git", args...); err != nil {
			return "", err
		}
	}
	body := "Stacked on " + base + "."
	create := []string{"pr", "create", "--head", work.Branch, "--base", base, "--title", "fix: stacked change"}
	url, err := run(work.Path, "gh", append(create, "--body", body)...)
	if err != nil {
		return "", err
	}
	args, _ := json.Marshal(map[string]string{"url": url, "title": "fix: stacked change"})
	if out, bad, err := c.Call("track_mr", args); err != nil || bad {
		return "", fmt.Errorf("track_mr: %v %v: %s", err, bad, out)
	}
	return "Stacked the fix. " + url, nil
}

func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Fixture Bot", "GIT_AUTHOR_EMAIL=bot@example.test", "GIT_COMMITTER_NAME=Fixture Bot", "GIT_COMMITTER_EMAIL=bot@example.test")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %v: %w: %s", name, args, err, stderr.String())
	}
	return strings.TrimSpace(string(b)), nil
}
