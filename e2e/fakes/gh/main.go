// Command gh is the offline GitHub CLI boundary: argv/env are logged next to the executable.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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
	args := strings.Join(os.Args[1:], " ")
	switch {
	case args == "auth status":
		return authStatus()
	case args == "api user":
		return apiUser()
	case strings.HasPrefix(args, "pr create "):
		return prCreate(os.Args[3:])
	case strings.HasPrefix(args, "pr ready "):
		return prReady(os.Args[3:])
	case strings.HasPrefix(args, "pr edit "):
		return prEdit(os.Args[3:])
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

// githubAPI calls the fake GitHub, whose URL is in the github-url file beside the executable. It
// sends the token real gh would use for github.com: GH_TOKEN, else GITHUB_TOKEN, else hosts.yml.
func githubAPI(method, path string, body any) ([]byte, int, error) {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token, _ = configToken()
	}
	if token == "" {
		return nil, 0, errors.New("gh: To get started with GitHub CLI, please run:  gh auth login")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, 0, err
	}
	base, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "github-url"))
	if err != nil {
		return nil, 0, fmt.Errorf("github-url beside the fake gh: %w", err)
	}
	var in io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		in = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimSpace(string(base))+path, in)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "token "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode, nil
}

// apiUser is `gh api user` against the fake GitHub. It prints the reply body, or fails as gh does
// on a rejected token.
func apiUser() error {
	body, status, err := githubAPI(http.MethodGet, "/user", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("gh: Bad credentials (HTTP %d)", status)
	}
	fmt.Println(string(body))
	return nil
}

// prCreate is `gh pr create --head <branch> [--base <branch>] --title <t> [--body <b>] [--draft]`, the
// flags a Bot uses. Like gh it opens the pull request in the repo named by the origin of the current
// directory (the last two path segments of its URL), against base main when --base is absent, and
// prints the pull request's URL. --draft opens it as a draft.
func prCreate(args []string) error {
	in := map[string]any{"base": "main"}
	for i := 0; i < len(args); i++ {
		if args[i] == "--draft" {
			in["draft"] = true
			continue
		}
		key, ok := strings.CutPrefix(args[i], "--")
		if !ok || i+1 == len(args) || !strings.Contains(" head base title body ", " "+key+" ") {
			return fmt.Errorf("unsupported gh pr create argv %q", args)
		}
		i++
		in[key] = args[i]
	}
	if in["head"] == nil || in["title"] == nil {
		return errors.New("gh: pr create needs --head and --title")
	}
	origin, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return fmt.Errorf("gh: no git remote origin here: %w", err)
	}
	parts := strings.FieldsFunc(strings.TrimSuffix(strings.TrimSpace(string(origin)), ".git"), func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) < 2 {
		return fmt.Errorf("gh: cannot read owner/repo from origin %q", origin)
	}
	body, status, err := githubAPI(http.MethodPost, "/repos/"+parts[len(parts)-2]+"/"+parts[len(parts)-1]+"/pulls", in)
	if err != nil {
		return err
	}
	if status != http.StatusCreated {
		return fmt.Errorf("gh: HTTP %d: %s", status, bytes.TrimSpace(body))
	}
	var pr struct {
		URL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &pr); err != nil || pr.URL == "" {
		return fmt.Errorf("gh: no html_url in %q", body)
	}
	fmt.Println(pr.URL)
	return nil
}

// repoPath is "owner/repo" of the origin of the current directory, as gh reads it.
func repoPath() (string, error) {
	origin, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("gh: no git remote origin here: %w", err)
	}
	parts := strings.FieldsFunc(strings.TrimSuffix(strings.TrimSpace(string(origin)), ".git"), func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) < 2 {
		return "", fmt.Errorf("gh: cannot read owner/repo from origin %q", origin)
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1], nil
}

// prReady is `gh pr ready <number>`: like gh it calls the GraphQL mutation markPullRequestReadyForReview
// with the pull request's node id, here PR_<owner>/<repo>#<number>.
func prReady(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("unsupported gh pr ready argv %q", args)
	}
	repo, err := repoPath()
	if err != nil {
		return err
	}
	query := "mutation PullRequestReadyForReview($input: MarkPullRequestReadyForReviewInput!) { markPullRequestReadyForReview(input: $input) { pullRequest { isDraft } } }"
	body, status, err := githubAPI(http.MethodPost, "/graphql", map[string]any{"query": query,
		"variables": map[string]any{"input": map[string]string{"pullRequestId": "PR_" + repo + "#" + args[0]}}})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("gh: HTTP %d: %s", status, bytes.TrimSpace(body))
	}
	return nil
}

// prEdit is `gh pr edit <number> --base <branch>`: a PATCH of the pull request's base.
func prEdit(args []string) error {
	if len(args) != 3 || args[1] != "--base" {
		return fmt.Errorf("unsupported gh pr edit argv %q", args)
	}
	repo, err := repoPath()
	if err != nil {
		return err
	}
	body, status, err := githubAPI(http.MethodPatch, "/repos/"+repo+"/pulls/"+args[0], map[string]string{"base": args[2]})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("gh: HTTP %d: %s", status, bytes.TrimSpace(body))
	}
	return nil
}
