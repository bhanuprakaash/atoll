package fake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitHubProbe is what a run of a Bot has for GitHub, found with the env the server gave this
// process. The fake GitHub's URL is in the github-url file beside the executable. It runs
// `gh api user`, `git credential fill` for that URL and for host git.example, and a real
// `git push` of one commit to <url>/octo/hello.git, then calls get_me on the run's `github` MCP
// server when it has one. The reply is JSON: gh, git, other_git and push as {exit, output}, and
// mcp as {connected, text}. A reply is stored with the run, so it never carries a secret: in the
// credential output each password=<x> line becomes password_sha256=<first 12 hex of sha256(x)>, and
// any GH_TOKEN value elsewhere is replaced by <GH_TOKEN>.
func GitHubProbe(c *MCP) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "github-url"))
	if err != nil {
		return "", fmt.Errorf("github-url beside the fake: %w", err)
	}
	base := strings.TrimSpace(string(b))
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	work, err := os.MkdirTemp("", "github-probe-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(work) }()

	git := func(args ...string) *exec.Cmd {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Fixture Bot", "GIT_AUTHOR_EMAIL=bot@example.test",
			"GIT_COMMITTER_NAME=Fixture Bot", "GIT_COMMITTER_EMAIL=bot@example.test")
		return cmd
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "probe.txt"}, {"commit", "-m", "probe"}} {
		if args[0] == "add" {
			if err := os.WriteFile(filepath.Join(work, "probe.txt"), []byte("pushed by the probe\n"), 0o644); err != nil {
				return "", err
			}
		}
		if out, err := git(args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git %v: %w: %s", args, err, out)
		}
	}
	credential := func(protocol, host string) *exec.Cmd {
		cmd := git("credential", "fill")
		cmd.Stdin = strings.NewReader("protocol=" + protocol + "\nhost=" + host + "\n\n")
		return cmd
	}
	reply := map[string]any{}
	for name, cmd := range map[string]*exec.Cmd{
		"gh":        exec.Command("gh", "api", "user"),
		"git":       credential(u.Scheme, u.Host),
		"other_git": credential("https", "git.example"),
		"push":      git("push", base+"/octo/hello.git", "HEAD:refs/heads/reef-push"),
	} {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		p, err := runProbe(cmd)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		p.Output = redact(p.Output, strings.HasSuffix(name, "git") && name != "push")
		reply[name] = p
	}
	mcp := map[string]any{"connected": c != nil}
	if c != nil {
		text, bad, err := c.Call("get_me", json.RawMessage(`{}`))
		if err != nil || bad {
			return "", fmt.Errorf("github get_me: %v %v: %s", err, bad, text)
		}
		mcp["text"] = redact(text, false)
	}
	reply["mcp"] = mcp
	out, err := json.Marshal(reply)
	return string(out), err
}

func runProbe(cmd *exec.Cmd) (LoginProbe, error) {
	out, err := cmd.CombinedOutput()
	p := LoginProbe{Output: string(out)}
	if exit, ok := err.(*exec.ExitError); ok {
		p.Exit = exit.ExitCode()
		return p, nil
	}
	return p, err
}

// redact keeps a secret out of what a run stores: see GitHubProbe.
func redact(out string, credential bool) string {
	if credential {
		var lines []string
		for line := range strings.Lines(out) {
			if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "password="); ok {
				sum := sha256.Sum256([]byte(v))
				line = "password_sha256=" + hex.EncodeToString(sum[:])[:12] + "\n"
			}
			lines = append(lines, line)
		}
		out = strings.Join(lines, "")
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		out = strings.ReplaceAll(out, token, "<GH_TOKEN>")
	}
	return out
}
