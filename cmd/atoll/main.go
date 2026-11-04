// Command atoll is the Reef server.
package main

import (
	"context"
	"errors"
	"flag"
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
  atoll serve [--addr host:port]
  atoll provider status
  atoll provider run <claude|codex> [--resume <id>] <prompt>
  atoll provider login <claude|codex>`

func usage() error { return errors.New(_usage) }

type adapter struct {
	name     string
	new      func() provider.Session
	signedIn func(ctx context.Context, bin, dir string) (bool, error)
	login    func(ctx context.Context, bin, dir string) *exec.Cmd
}

var _adapters = []adapter{
	{
		name:     "claude",
		new:      func() provider.Session { return claude.New() },
		signedIn: claude.SignedIn,
		login:    claude.Login,
	},
	{
		name:     "codex",
		new:      func() provider.Session { return codex.New() },
		signedIn: codex.SignedIn,
		login:    codex.Login,
	},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintln(w, "atoll")
		return err
	}
	if args[0] == "serve" {
		return serve(ctx, args[1:], w)
	}
	if len(args) < 2 || args[0] != "provider" {
		return usage()
	}
	if args[1] == "status" && len(args) == 2 {
		return status(ctx, w)
	}
	if len(args) < 3 {
		return usage()
	}
	var a *adapter
	for i := range _adapters {
		if _adapters[i].name == args[2] {
			a = &_adapters[i]
		}
	}
	switch {
	case a == nil:
		return usage()
	case args[1] == "run":
		return a.chat(ctx, args[3:], w)
	case args[1] == "login" && len(args) == 3:
		return a.signIn(ctx)
	}
	return usage()
}

func status(ctx context.Context, w io.Writer) error {
	dirs := pathDirs(ctx)
	for _, a := range _adapters {
		bin := a.find(dirs)
		if bin == "" {
			if _, err := fmt.Fprintf(w, "%s\tnot installed\n", a.name); err != nil {
				return err
			}
			continue
		}
		dir, err := a.dir()
		if err != nil {
			return err
		}
		line, err := a.describe(ctx, bin, dir)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			line = "error: " + err.Error()
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", a.name, bin, line); err != nil {
			return err
		}
	}
	return nil
}

// describe returns a's version and sign-in state, tab separated.
func (a adapter) describe(ctx context.Context, bin, dir string) (string, error) {
	version, err := provider.Version(ctx, bin)
	if err != nil {
		return "", err
	}
	in, err := a.signedIn(ctx, bin, dir)
	if err != nil {
		return "", fmt.Errorf("sign-in: %w", err)
	}
	state := "signed out"
	if in {
		state = "signed in"
	}
	return version + "\t" + state, nil
}

// chat sends one prompt and prints the reply, then the session id.
func (a adapter) chat(ctx context.Context, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	resume := fs.String("resume", "", "session id to resume")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usage()
	}
	bin, dir, err := a.prepare(ctx)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg := provider.Config{Bin: bin, Dir: dir, Cwd: cwd}
	s := a.new()
	if *resume == "" {
		err = s.Start(ctx, cfg)
	} else {
		err = s.Resume(ctx, cfg, *resume)
	}
	if err != nil {
		return fmt.Errorf("start %s: %w", a.name, err)
	}
	defer func() { _ = s.Interrupt() }()
	if err := s.Send(ctx, fs.Arg(0)); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	var reply strings.Builder
	var session string
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-s.Events():
			if !ok {
				return fmt.Errorf("%s exited before the turn ended", a.name)
			}
			switch ev.Kind {
			case provider.KindText:
				reply.WriteString(ev.Text)
			case provider.KindSession:
				session = ev.SessionID
			case provider.KindError:
				return errors.New(ev.Text)
			case provider.KindTurnEnd:
				_, err := fmt.Fprintf(w, "%s\nsession: %s\n", strings.TrimSpace(reply.String()), session)
				return err
			}
		}
	}
}

func (a adapter) signIn(ctx context.Context) error {
	bin, dir, err := a.prepare(ctx)
	if err != nil {
		return err
	}
	cmd := a.login(ctx, bin, dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func (a adapter) prepare(ctx context.Context) (bin, dir string, err error) {
	if bin = a.find(pathDirs(ctx)); bin == "" {
		return "", "", fmt.Errorf("%s is not installed", a.name)
	}
	if dir, err = a.dir(); err != nil {
		return "", "", err
	}
	return bin, dir, os.MkdirAll(dir, 0o700)
}

func (a adapter) find(dirs []string) string {
	override := os.Getenv("ATOLL_" + strings.ToUpper(a.name) + "_PATH")
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
		return data, nil
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "Atoll"), nil
}
