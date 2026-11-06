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
	if err := checkOAuthConfig(os.Getenv("GLAB_CONFIG_DIR")); err != nil {
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

// checkOAuthConfig mirrors real glab: an OAuth host without an expiry date is a config error.
func checkOAuthConfig(dir string) error {
	if dir == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		return err
	}
	oauth, expiry := false, ""
	for line := range strings.Lines(string(b)) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "is_oauth2":
			oauth = value == "true"
		case "oauth2_expiry_date":
			expiry = value
		}
	}
	if oauth && expiry == "" {
		return fmt.Errorf("could not parse \"\" as an expiry date")
	}
	return nil
}
