package fake

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ProcsHold is a run that has its logins and MCP servers and stays live. It calls get_me on the
// `github` MCP server and the `answer` tool of `docs-remote`, runs GitHubProbe, writes
// <executable>.procs.<pid> with the URL of its `atoll` server (which holds the run's token), and
// waits for <executable>.procs.<pid>.release. The reply is JSON: github (the GitHubProbe reply)
// and docs (the text `answer` returned).
func ProcsHold(servers map[string]*MCP) (string, error) {
	for _, name := range []string{"atoll", "github", "docs-remote"} {
		if servers[name] == nil {
			return "", fmt.Errorf("MCP server %s not connected", name)
		}
	}
	github, err := GitHubProbe(servers["github"])
	if err != nil {
		return "", err
	}
	docs, bad, err := servers["docs-remote"].Call("answer", json.RawMessage(`{}`))
	if err != nil || bad {
		return "", fmt.Errorf("docs-remote answer: %q %v", docs, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	held := exe + ".procs." + strconv.Itoa(os.Getpid())
	url, _ := json.Marshal(map[string]string{"atoll_url": servers["atoll"].URL()})
	if err := os.WriteFile(held, url, 0o600); err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(held + ".release"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, err := json.Marshal(map[string]any{"github": json.RawMessage(github), "docs": docs})
	return string(b), err
}

var _needles = regexp.MustCompile(`needles=(\S+)`)

// _scan is a Bot's shell command: it prints "<pid> environ <NAME>" for each variable of each
// process whose NAME=value line holds a line of the needles file, and "<pid> cmdline" for each
// command line that holds one. The needles reach grep only through the file, so no process of
// the scan holds one.
const _scan = `for p in /proc/[0-9]*; do
	pid=${p#/proc/}
	tr '\0' '\n' <"$p/environ" 2>/dev/null | grep -aF -f "$1" | cut -d= -f1 | sed "s/^/$pid environ /"
	tr '\0' ' ' <"$p/cmdline" 2>/dev/null | grep -aqF -f "$1" && echo "$pid cmdline"
done; true`

// ProcsScan runs _scan with the file named by "needles=<path>" in the prompt and returns the
// findings as a JSON array of {pid, kind, name}. A reply never carries a needle.
func ProcsScan(prompt string) (string, error) {
	m := _needles.FindStringSubmatch(prompt)
	if m == nil {
		return "", fmt.Errorf("no needles=<path> in %q", prompt)
	}
	out, err := exec.Command("sh", "-c", _scan, "sh", m[1]).Output()
	if err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}
	type finding struct {
		PID  int    `json:"pid"`
		Kind string `json:"kind"`
		Name string `json:"name,omitempty"`
	}
	found := []finding{}
	for line := range strings.Lines(string(out)) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			return "", fmt.Errorf("scan line %q", line)
		}
		x := finding{PID: pid, Kind: f[1]}
		if len(f) > 2 {
			x.Name = f[2]
		}
		found = append(found, x)
	}
	b, err := json.Marshal(found)
	return string(b), err
}
