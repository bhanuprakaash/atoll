package fake

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MRURL is returned by the offline glab fixture.
const MRURL = "https://gitlab.example/fixtures/seeded/-/merge_requests/1"

// RepoStep performs real local git operations; only the provider and the GitLab and GitHub CLIs
// are fakes. Mode github-fix is fix for a GitHub repo: it opens the pull request with gh, gives
// track_mr the title too, and when start_work fails replies with the error text.
func RepoStep(c *MCP, mode, cwd string) (string, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	git := func(dir string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Fixture Bot", "GIT_AUTHOR_EMAIL=bot@example.test", "GIT_COMMITTER_NAME=Fixture Bot", "GIT_COMMITTER_EMAIL=bot@example.test")
		b, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %v: %w: %s", args, err, b)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if mode == "question" || mode == "resume" {
		b, err := os.ReadFile(filepath.Join(cwd, "answer.txt"))
		if err != nil {
			return "", err
		}
		status, err := git(cwd, "status", "--porcelain")
		if err != nil {
			return "", err
		}
		if status != "" {
			return "", fmt.Errorf("read-only checkout is dirty")
		}
		return fmt.Sprintf("Read %s: %s", cwd, strings.TrimSpace(string(b))), nil
	}
	if c == nil {
		return "", fmt.Errorf("atoll MCP server absent")
	}
	out, bad, err := c.Call("start_work", json.RawMessage(`{"type":"fix","slug":"Seeded BUG"}`))
	if err != nil {
		return "", err
	}
	if bad && mode == "github-fix" {
		return "start_work failed: " + out, nil
	}
	if bad {
		return "", fmt.Errorf("start_work: %s", out)
	}
	startWork := out
	var work struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal([]byte(out), &work); err != nil {
		return "", fmt.Errorf("start_work: %w", err)
	}
	if !filepath.IsAbs(work.Path) || work.Branch == "" {
		return "", fmt.Errorf("start_work returned no absolute path/branch: %s", out)
	}
	if err := os.WriteFile(filepath.Join(work.Path, "answer.txt"), []byte("42\n"), 0o644); err != nil {
		return "", err
	}
	if mode == "uncommitted" {
		if err := os.WriteFile(filepath.Join(work.Path, "unfinished.txt"), []byte("keep this untracked work\n"), 0o644); err != nil {
			return "", err
		}
		return "Left tracked and untracked edits unfinished.\nstart_work result: " + startWork, nil
	}
	for _, args := range [][]string{{"add", "answer.txt"}, {"commit", "-m", "fix: correct seeded answer"}, {"push", "-u", "origin", work.Branch}} {
		if _, err := git(work.Path, args...); err != nil {
			return "", err
		}
	}
	cmd := exec.Command("glab", "mr", "create", "--title", "fix: correct seeded answer", "--description", "Correct the fixture answer.", "--source-branch", work.Branch)
	trackArgs := map[string]string{}
	if mode == "github-fix" {
		cmd = exec.Command("gh", "pr", "create", "--head", work.Branch, "--base", "main", "--title", "fix: correct seeded answer", "--body", "Correct the fixture answer.")
		trackArgs["title"] = "fix: correct seeded answer"
	}
	cmd.Dir = work.Path
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(cmd.Args[:3], " "), err, b)
	}
	url := strings.TrimSpace(string(b))
	trackArgs["url"] = url
	args, _ := json.Marshal(trackArgs)
	out, bad, err = c.Call("track_mr", args)
	if err != nil {
		return "", err
	}
	if bad {
		return "", fmt.Errorf("track_mr: %s", out)
	}
	return "Fixed the seeded bug. " + url + "\nstart_work result: " + startWork, nil
}
