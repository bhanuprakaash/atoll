// Package fake is what the fake claude and codex share: the call log and the recordings.
package fake

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// _testdata is e2e/testdata. The harness sets it with -ldflags -X, so the fake needs no env
// that atoll might not pass on.
var _testdata string

// Record is one line of the call log: the first of a process has its argv and env, each later
// one a line it read on stdin.
type Record struct {
	PID   int      `json:"pid"`
	Argv  []string `json:"argv,omitempty"`
	Env   []string `json:"env,omitempty"`
	Stdin string   `json:"stdin,omitempty"`
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
	return l, l.write(Record{Argv: os.Args[1:], Env: os.Environ()})
}

// Stdin records one line read on stdin.
func (l *Log) Stdin(line string) error {
	return l.write(Record{Stdin: line})
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
