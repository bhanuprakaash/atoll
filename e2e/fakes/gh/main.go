// Command gh is the offline GitHub CLI boundary: argv/env are logged next to the executable.
package main

import (
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
	if strings.Join(os.Args[1:], " ") != "auth status" {
		return fmt.Errorf("unsupported gh argv %q", os.Args[1:])
	}
	return authStatus()
}

// authStatus looks for a login where real gh does: a token in the env, else an oauth_token in
// hosts.yml of $GH_CONFIG_DIR, else $XDG_CONFIG_HOME/gh, else ~/.config/gh.
func authStatus() error {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		if os.Getenv(key) != "" {
			fmt.Println("github.com\n  Logged in to github.com account fake-user (" + key + ")")
			return nil
		}
	}
	dir := os.Getenv("GH_CONFIG_DIR")
	if dir == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dir = filepath.Join(xdg, "gh")
		} else {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".config", "gh")
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "hosts.yml")); err == nil {
		for line := range strings.Lines(string(b)) {
			if key, value, _ := strings.Cut(strings.TrimSpace(line), ":"); key == "oauth_token" && strings.Trim(strings.TrimSpace(value), `"'`) != "" {
				fmt.Println("github.com\n  Logged in to github.com account fake-user (" + filepath.Join(dir, "hosts.yml") + ")")
				return nil
			}
		}
	}
	return errors.New("not logged in to any GitHub host: run gh auth login")
}
