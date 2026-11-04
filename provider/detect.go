package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	_pathEnv      = "ATOLL_PROVIDER_PATH"
	_shellTimeout = 3 * time.Second
)

var _versionRe = regexp.MustCompile(`^\d+(\.\d+)+$`)

// Dirs lists the folders to search for provider executables, in order. When ATOLL_PROVIDER_PATH is
// set it is the only source. Otherwise it is the login shell's PATH, then the install folders of
// Claude Code and Codex: a Finder-launched app lacks the shell PATH.
func Dirs(ctx context.Context) []string {
	if p, ok := os.LookupEnv(_pathEnv); ok {
		return dedupe(filepath.SplitList(p))
	}
	dirs := shellPath(ctx)
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".claude", "local"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".bun", "bin"),
		)
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	return dedupe(dirs)
}

// Find returns the path of the provider executable name: override when it is an executable file,
// else the first match in dirs. It returns "" when there is none.
func Find(name, override string, dirs []string) string {
	if override != "" && isExecutable(override) {
		return override
	}
	for _, dir := range dirs {
		if p := filepath.Join(dir, name); isExecutable(p) {
			return p
		}
	}
	return ""
}

// Version returns the version number bin reports for --version.
func Version(ctx context.Context, bin string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("run %s --version: %w", bin, err)
	}
	for _, f := range strings.Fields(string(out)) {
		if _versionRe.MatchString(f) {
			return f, nil
		}
	}
	return "", errors.New("no version in " + strings.TrimSpace(string(out)))
}

// shellPath returns the PATH of the user's login shell, or nil if it cannot be read.
func shellPath(ctx context.Context) []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, _shellTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", "printenv PATH")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return filepath.SplitList(strings.TrimSpace(lines[len(lines)-1]))
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func dedupe(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	var out []string
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
