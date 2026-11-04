// Command glab is the offline GitLab CLI boundary: argv/env are logged next to the executable.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"atoll/e2e/fakes/fake"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if _, err := fake.Start(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := strings.Join(os.Args[1:], " ")
	switch {
	case strings.HasPrefix(args, "mr create"):
		fmt.Println(fake.MRURL)
	case strings.HasPrefix(args, "mr view"):
		b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "mr-state"))
		if err != nil {
			return err
		}
		state := strings.TrimSpace(string(b))
		if state != "opened" && state != "merged" {
			return fmt.Errorf("unknown MR state %q", state)
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"iid": 1, "state": state, "web_url": fake.MRURL, "source_branch": "fix/seeded-bug"})
	default:
		return fmt.Errorf("unsupported glab argv %q", os.Args[1:])
	}
	return nil
}
