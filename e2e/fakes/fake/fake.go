// Package fake is what the fake claude and codex share: the call log and the recordings.
package fake

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// _testdata is e2e/testdata. The harness sets it with -ldflags -X, so the fake needs no env
// that atoll might not pass on.
var _testdata string

// Record is one line of the call log: the first of a process has its cwd, argv and env; later
// records hold data the fake loaded or a line it read on stdin.
type Record struct {
	PID       int             `json:"pid"`
	Cwd       string          `json:"cwd,omitempty"`
	Argv      []string        `json:"argv,omitempty"`
	Env       []string        `json:"env,omitempty"`
	Dirs      map[string]Dir  `json:"dirs,omitempty"`
	MCPConfig json.RawMessage `json:"mcpConfig,omitempty"`
	Stdin     string          `json:"stdin,omitempty"`
}

// Dir is what a folder named by an env var looked like when the process started: the nearest
// folder that existed (the folder itself when it did) and whether a file could be made in it.
type Dir struct {
	Existing string `json:"existing"`
	Writable bool   `json:"writable"`
}

var _probed = []string{"XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "GOCACHE", "GOLANGCI_LINT_CACHE", "pnpm_config_store_dir", "PNPM_HOME"}

func probe() map[string]Dir {
	dirs := map[string]Dir{}
	for _, name := range _probed {
		p := os.Getenv(name)
		if !filepath.IsAbs(p) {
			continue
		}
		for {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				break
			}
			p = filepath.Dir(p)
		}
		d := Dir{Existing: p}
		if f, err := os.CreateTemp(p, ".probe-*"); err == nil {
			d.Writable = true
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
		dirs[name] = d
	}
	return dirs
}

// Log appends Records to <executable>.log.
type Log struct {
	f *os.File
}

// Start opens the log and records this process's argv and env.
func Start() (*Log, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(exe+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	l := &Log{f: f}
	cwd, _ := os.Getwd()
	return l, l.write(Record{Cwd: cwd, Argv: os.Args[1:], Env: os.Environ(), Dirs: probe()})
}

// Stdin records one line read on stdin.
func (l *Log) Stdin(line string) error {
	return l.write(Record{Stdin: line})
}

// MCPConfig records the config a fake provider loaded after resolving it.
func (l *Log) MCPConfig(config any) error {
	b, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return l.write(Record{MCPConfig: b})
}

// Trace is one line of <executable>.log.trace: a line the fake read from Reef or printed to it, in
// the order it happened. Kind is "read" or "emit". For a user line Detail is "user", UUID its
// uuid ("" when it carried none) and Text the start of its text; for a printed line Detail is
// "result" ("result+origin" when it has an origin) or "command_lifecycle:<state>" with UUID the
// command_uuid.
type Trace struct {
	PID    int       `json:"pid"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail"`
	UUID   string    `json:"uuid"`
	Text   string    `json:"text,omitempty"`
}

// Trace appends t to <executable>.log.trace, a file apart from the log so no reader of that log
// sees it.
func (l *Log) Trace(t Trace) error {
	f, err := os.OpenFile(l.f.Name()+".trace", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	t.PID, t.At = os.Getpid(), time.Now()
	return json.NewEncoder(f).Encode(t)
}

func (l *Log) write(r Record) error {
	r.PID = os.Getpid()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = l.f.Write(append(b, '\n'))
	return err
}

// Recordings returns the lines of every recording of provider, by file name without .jsonl.
func Recordings(provider string) (map[string][]string, error) {
	paths, err := filepath.Glob(filepath.Join(_testdata, provider, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no recordings in %s", filepath.Join(_testdata, provider))
	}
	recs := make(map[string][]string, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		recs[strings.TrimSuffix(filepath.Base(p), ".jsonl")] = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	return recs, nil
}

// Lines returns a scanner over stdin that takes lines of any length.
func Lines() *bufio.Scanner {
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	return s
}

// ConfigDir is the provider's settings folder: the value of env, or dir under HOME.
func ConfigDir(env, dir string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, dir)
}

// Tools records, as a prompt of kind "tools", the tool names each connected MCP server listed to
// this process, as JSON {server: [name, ...]} with the names sorted.
func (l *Log) Tools(servers map[string]*MCP) error {
	listed := map[string][]string{}
	for name, c := range servers {
		listed[name] = slices.Sorted(maps.Keys(c.Tools))
	}
	b, err := json.Marshal(listed)
	if err != nil {
		return err
	}
	return l.Prompt("tools", string(b))
}

// Prompt records decoded additions separately so the original argv/stdin log stays intact.
func (l *Log) Prompt(kind, text string) error {
	f, err := os.OpenFile(l.f.Name()+".prompts", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return json.NewEncoder(f).Encode(struct {
		PID  int    `json:"pid"`
		Kind string `json:"kind"`
		Text string `json:"text"`
	}{PID: os.Getpid(), Kind: kind, Text: text})
}
