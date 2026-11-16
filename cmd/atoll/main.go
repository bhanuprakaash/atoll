// Command atoll is the Reef server.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"atoll/provider"
	"atoll/provider/claude"
	"atoll/provider/codex"
)

const _usage = `usage:
  atoll serve [--addr host:port]`

func usage() error { return errors.New(_usage) }

type adapter struct {
	name     string
	new      func() provider.Session
	signedIn func(ctx context.Context, bin, dir string) (bool, error)
	login    func(ctx context.Context, bin, dir string) *exec.Cmd
	own      func(home string) provider.Own
}

var _adapters = []adapter{
	{
		name:     "claude",
		new:      func() provider.Session { return claude.New() },
		signedIn: claude.SignedIn,
		login:    claude.Login,
		own:      claude.Own,
	},
	{
		name:     "codex",
		new:      func() provider.Session { return codex.New() },
		signedIn: codex.SignedIn,
		login:    codex.Login,
		own:      codex.Own,
	},
}

func main() {
	token, err := scrubEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(ctx, token, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, token string, args []string, w io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintln(w, "atoll")
		return err
	}
	if args[0] == "serve" {
		return serve(ctx, token, args[1:], w)
	}
	return usage()
}

func (a adapter) prepare(ctx context.Context, override string) (bin, dir string, err error) {
	if bin = a.find(pathDirs(ctx), override); bin == "" {
		return "", "", provider.Off(fmt.Errorf("%s is not installed", a.name))
	}
	if dir, err = a.dir(); err != nil {
		return "", "", err
	}
	return bin, dir, os.MkdirAll(dir, 0o700)
}

// find returns the executable at override, else at the ATOLL_<NAME>_PATH override, else the first
// match in dirs.
func (a adapter) find(dirs []string, override string) string {
	if override == "" {
		override = os.Getenv("ATOLL_" + strings.ToUpper(a.name) + "_PATH")
	}
	return provider.Find(a.name, override, dirs)
}

// pathDirs lists the provider folders and puts them first on PATH, so that every child process
// finds what the provider needs, such as node for a script with an env shebang.
func pathDirs(ctx context.Context) []string {
	dirs := provider.Dirs(ctx)
	var path []string
	for _, d := range slices.Concat(dirs, filepath.SplitList(os.Getenv("PATH"))) {
		if d != "" && !slices.Contains(path, d) {
			path = append(path, d)
		}
	}
	_ = os.Setenv("PATH", strings.Join(path, string(os.PathListSeparator)))
	return dirs
}

func (a adapter) dir() (string, error) {
	data, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(data, "providers", a.name), nil
}

func dataDir() (string, error) {
	if data := os.Getenv("ATOLL_DATA_DIR"); data != "" {
		return filepath.Abs(data)
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "Atoll"), nil
}
