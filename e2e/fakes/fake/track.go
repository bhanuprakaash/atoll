package fake

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TrackStep is a Bot with no worktree of its own tracking a pull request it did not push from one:
// from its read-only checkout it pushes a new commit to the branch boss-pushed-<checkout folder>, as a boss pushes from
// a helper's worktree, opens the pull request with gh and tracks it. It never calls start_work.
func TrackStep(c *MCP, cwd string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("track step needs the atoll MCP server")
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	branch := "boss-pushed-" + filepath.Base(cwd)
	commit, err := run(cwd, "git", "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "fix: pushed by the boss\n\n"+cwd)
	if err != nil {
		return "", err
	}
	if _, err := run(cwd, "git", "push", "origin", commit+":refs/heads/"+branch); err != nil {
		return "", err
	}
	url, err := run(cwd, "gh", "pr", "create", "--head", branch, "--base", "main", "--title", "fix: pushed by the boss", "--body", "Pushed from a helper's worktree.")
	if err != nil {
		return "", err
	}
	args, _ := json.Marshal(map[string]string{"url": url, "title": "fix: pushed by the boss"})
	if out, bad, err := c.Call("track_mr", args); err != nil || bad {
		return "", fmt.Errorf("track_mr: %v %v: %s", err, bad, out)
	}
	return "Tracked the pull request. " + url, nil
}
