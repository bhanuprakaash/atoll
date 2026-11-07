// Command glab is the offline GitLab CLI boundary: argv/env are logged next to the executable.
package main

import (
	"encoding/json"
	"errors"
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
	if args == "auth status" {
		return authStatus()
	}
	if err := checkOAuthConfig(os.Getenv("GLAB_CONFIG_DIR")); err != nil {
		return err
	}
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

// authStatus looks for a login where real glab does: a token in the env, else a token in
// config.yml of $GLAB_CONFIG_DIR, else $XDG_CONFIG_HOME/glab-cli, else ~/.config/glab-cli.
func authStatus() error {
	for _, key := range []string{"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"} {
		if os.Getenv(key) != "" {
			fmt.Println("gitlab.com\n  Logged in to gitlab.com as fake-user (" + key + ")")
			return nil
		}
	}
	dir := os.Getenv("GLAB_CONFIG_DIR")
	if dir == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dir = filepath.Join(xdg, "glab-cli")
		} else {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".config", "glab-cli")
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "config.yml")); err == nil {
		for line := range strings.Lines(string(b)) {
			if key, value, _ := strings.Cut(strings.TrimSpace(line), ":"); key == "token" && strings.Trim(strings.TrimSpace(value), `"'`) != "" {
				fmt.Println("gitlab.com\n  Logged in to gitlab.com as fake-user (" + filepath.Join(dir, "config.yml") + ")")
				return nil
			}
		}
	}
	return errors.New("no token found for gitlab.com: run glab auth login")
}
