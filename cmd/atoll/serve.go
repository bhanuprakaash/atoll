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
	"time"

	"atoll/memory"
	"atoll/provider"
	threadrun "atoll/run"
	"atoll/server"
	"atoll/store"
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
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usage()
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	data, err := dataDir()
	if err != nil {
		return err
	}
	st, err := store.Open(data)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	chiefProvider := "claude"
	dirs := pathDirs(ctx)
	for _, adapter := range _adapters {
		bin := adapter.find(dirs)
		if bin == "" {
			continue
		}
		dir, err := adapter.dir()
		if err != nil {
			continue
		}
		signed, err := adapter.signedIn(ctx, bin, dir)
		if err == nil && signed {
			chiefProvider = adapter.name
			break
		}
	}
	if _, err := st.EnsureBot(ctx, store.Bot{ID: "chief", Name: "Chief", Persona: "I help configure Reef.", Provider: chiefProvider, CanManagePlatform: true}); err != nil {
		return err
	}
	mem, err := memory.Open(st.DB(), nil)
	if err != nil {
		return err
	}
	failed, err := st.FailUnfinished(ctx, _restartReason)
	if err != nil {
		return err
	}
	for _, id := range failed {
		log.Warn("run failed by restart", "run_id", id)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	runs := threadrun.New(threadrun.Config{
		Store:        st,
		Memory:       mem,
		Launch:       launch,
		MCPURL:       url + server.MCPPath,
		DataDir:      data,
		WorktreeRoot: filepath.Join(home, "Reef", "worktrees"),
		PollInterval: pollInterval(),
		Log:          log,
	})
	pathDirs(ctx) // glab is found on the provider folders' PATH
	runs.Start()
	srv := &http.Server{Handler: server.New(st, mem, runs, log), ReadHeaderTimeout: 10 * time.Second}
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

// pollInterval is ATOLL_REPO_POLL_INTERVAL, a Go duration, or a minute when it is unset, invalid
// or not positive.
func pollInterval() time.Duration {
	d, err := time.ParseDuration(os.Getenv("ATOLL_REPO_POLL_INTERVAL"))
	if err != nil || d <= 0 {
		return time.Minute
	}
	return d
}

// launch returns a session for the provider name with its binary and settings folder.
func launch(ctx context.Context, name string) (provider.Session, provider.Config, error) {
	for _, a := range _adapters {
		if a.name != name {
			continue
		}
		bin, dir, err := a.prepare(ctx)
		if err != nil {
			return nil, provider.Config{}, err
		}
		return a.new(), provider.Config{Bin: bin, Dir: dir}, nil
	}
	return nil, provider.Config{}, fmt.Errorf("unknown provider %q", name)
}
