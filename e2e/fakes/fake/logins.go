package fake

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// LoginProbe is what one CLI said when a run asked for its login.
type LoginProbe struct {
	Exit   int    `json:"exit"`
	Output string `json:"output"`
}

// LoginsProbe runs, in the run's own env, `glab auth status`, `gh auth status` and
// `git credential fill` for host git.example, and returns their exit codes and output as JSON.
func LoginsProbe() (string, error) {
	probes := map[string]LoginProbe{}
	for name, cmd := range map[string]*exec.Cmd{
		"glab": exec.Command("glab", "auth", "status"),
		"gh":   exec.Command("gh", "auth", "status"),
		"git":  exec.Command("git", "credential", "fill"),
	} {
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if name == "git" {
			cmd.Stdin = strings.NewReader("protocol=https\nhost=git.example\n\n")
		}
		out, err := cmd.CombinedOutput()
		p := LoginProbe{Output: string(out)}
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			p.Exit = exit.ExitCode()
		case err != nil:
			return "", fmt.Errorf("%s: %w", name, err)
		}
		probes[name] = p
	}
	b, err := json.Marshal(probes)
	return string(b), err
}
