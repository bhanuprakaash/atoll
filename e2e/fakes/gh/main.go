// Command gh is the offline GitHub CLI boundary: argv/env are logged next to the executable.
package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
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
	switch strings.Join(os.Args[1:], " ") {
	case "auth status":
		return authStatus()
	case "api user":
		return apiUser()
	}
	return fmt.Errorf("unsupported gh argv %q", os.Args[1:])
}

// configToken is the oauth_token of hosts.yml in $GH_CONFIG_DIR, else $XDG_CONFIG_HOME/gh, else
// ~/.config/gh, with the file it came from; empty when there is none.
func configToken() (token, file string) {
	dir := os.Getenv("GH_CONFIG_DIR")
	if dir == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dir = filepath.Join(xdg, "gh")
		} else {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".config", "gh")
		}
	}
	file = filepath.Join(dir, "hosts.yml")
	if b, err := os.ReadFile(file); err == nil {
		for line := range strings.Lines(string(b)) {
			if key, value, _ := strings.Cut(strings.TrimSpace(line), ":"); key == "oauth_token" {
				if v := strings.Trim(strings.TrimSpace(value), `"'`); v != "" {
					return v, file
				}
			}
		}
	}
	return "", ""
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
	if _, file := configToken(); file != "" {
		fmt.Println("github.com\n  Logged in to github.com account fake-user (" + file + ")")
		return nil
	}
	return errors.New("not logged in to any GitHub host: run gh auth login")
}

// apiUser is `gh api user` against the fake GitHub, whose URL is in the github-url file beside the
// executable. It sends the token real gh would use for github.com: GH_TOKEN, else GITHUB_TOKEN,
// else hosts.yml. It prints the reply body, or fails as gh does on a rejected token.
func apiUser() error {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token, _ = configToken()
	}
	if token == "" {
		return errors.New("gh: To get started with GitHub CLI, please run:  gh auth login")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	base, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "github-url"))
	if err != nil {
		return fmt.Errorf("github-url beside the fake gh: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(string(base))+"/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gh: Bad credentials (HTTP %d)", resp.StatusCode)
	}
	fmt.Println(string(body))
	return nil
}
