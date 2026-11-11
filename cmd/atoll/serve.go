package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"atoll/memory"
	threadrun "atoll/run"
	"atoll/server"
	"atoll/store"

	"github.com/zalando/go-keyring"
)

const (
	_restartReason = "Atoll stopped while the run was going"
	_shutdownGrace = 5 * time.Second
)

// serve runs the API, the MCP tools and the UI on addr until ctx ends. Its first line on w,
// printed once it accepts requests, is "listening on <url>".
func serve(ctx context.Context, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:4317", "address to listen on")
	exitOnStdinClose := fs.Bool("exit-on-stdin-close", false, "stop when standard input closes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usage()
	}
	if *exitOnStdinClose {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		// The stdin read cannot be unblocked portably, so this goroutine cannot be joined.
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			cancel()
		}()
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if os.Getenv("ATOLL_SECRET_STORE_UNAVAILABLE") != "" {
		keyring.MockInitWithError(errors.New("secret store unavailable"))
	}
	data, err := dataDir()
	if err != nil {
		return err
	}
	if err := writeClaudeShellPrefix(data); err != nil {
		return err
	}
	st, err := store.Open(data)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	for _, adapter := range _adapters {
		dir, err := adapter.dir()
		if err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(dir, "runs")); err != nil {
			return err
		}
	}
	chief, err := st.Bot(ctx, "chief")
	if err != nil {
		chief = store.Bot{
			ID: "chief", Name: "Chief", Persona: "I help configure Reef.",
			Provider: chiefProvider(ctx), Look: "jellyfish", CanManagePlatform: true, RepoAccess: []string{},
		}
		if _, err := st.EnsureBot(ctx, chief); err != nil {
			return err
		}
	}
	if chief.RepoAccess == nil || len(chief.RepoAccess) > 0 {
		chief.RepoAccess = []string{}
		if _, err := st.UpdateBot(ctx, chief); err != nil {
			return err
		}
	}
	mem, err := memory.Open(st.DB(), nil)
	if err != nil {
		return err
	}
	failed, err := st.FailUnfinished(ctx, _restartReason, threadrun.RestartWakeText)
	if err != nil {
		return err
	}
	for _, id := range failed {
		log.Warn("run failed by restart", "run_id", id)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("%s is in use by another program: %w", *addr, err)
		}
		return err
	}
	url := "http://" + ln.Addr().String()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	providers := newProviders(ctx, st, _adapters)
	defer providers.Close()
	runs := threadrun.New(threadrun.Config{
		Store:        st,
		Memory:       mem,
		Launch:       providers.launch,
		Available:    providers.available,
		SignedOut:    providers.signOut,
		MCPURL:       url + server.MCPPath,
		DataDir:      data,
		WorktreeRoot: filepath.Join(home, "Reef", "worktrees"),
		PollInterval: pollInterval(),
		WakeWindow:   wakeWindow(),
		Log:          log,
	})
	pathDirs(ctx) // glab is found on the provider folders' PATH
	runs.Start()
	srv := &http.Server{Handler: server.New(st, mem, runs, log, providers), ReadHeaderTimeout: 10 * time.Second}
	var serveErr error
	served := make(chan struct{})
	go func() {
		serveErr = srv.Serve(ln)
		close(served)
	}()
	log.Info("serving", "url", url, "data_dir", data)
	_, err = fmt.Fprintln(w, "listening on "+url)
	if err == nil {
		select {
		case <-served:
		case <-ctx.Done():
		}
	}
	closeErr := runs.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), _shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		closeErr = errors.Join(closeErr, err)
	}
	<-served
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(err, serveErr, closeErr)
}

// chiefProvider is the first installed and signed-in provider, else claude.
func chiefProvider(ctx context.Context) string {
	dirs := pathDirs(ctx)
	for _, adapter := range _adapters {
		bin := adapter.find(dirs, "")
		if bin == "" {
			continue
		}
		dir, err := adapter.dir()
		if err != nil {
			continue
		}
		signed, err := adapter.signedIn(ctx, bin, dir)
		if err == nil && signed {
			return adapter.name
		}
	}
	return "claude"
}

// pollInterval is ATOLL_REPO_POLL_INTERVAL, a Go duration, or a minute when it is unset, invalid
// or not positive.
func pollInterval() time.Duration {
	d, err := time.ParseDuration(os.Getenv("ATOLL_REPO_POLL_INTERVAL"))
	if err != nil || d <= 0 {
		return time.Minute
	}
	return d
}

// wakeWindow is ATOLL_WAKE_WINDOW, a Go duration, or 20 seconds when it is unset, invalid or not
// positive.
func wakeWindow() time.Duration {
	d, err := time.ParseDuration(os.Getenv("ATOLL_WAKE_WINDOW"))
	if err != nil || d <= 0 {
		return 20 * time.Second
	}
	return d
}

func writeClaudeShellPrefix(data string) error {
	bin := filepath.Join(data, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(bin, ".claude-shell-prefix.tmp")
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(tmp, []byte("#!/bin/bash\nfor v in $(compgen -e | grep -E '^(CLAUDE|ATOLL_MCP_)'); do unset \"$v\"; done\nexec bash -c \"$*\"\n"), 0o500); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o500); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(bin, "claude-shell-prefix")); err != nil {
		return err
	}
	return nil
}
