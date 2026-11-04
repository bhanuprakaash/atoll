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
	"time"

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
	runs := threadrun.New(threadrun.Config{
		Store:   st,
		Launch:  launch,
		MCPURL:  url + server.MCPPath,
		DataDir: data,
		Log:     log,
	})
	srv := &http.Server{Handler: server.New(st, runs, log), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	log.Info("serving", "url", url, "data_dir", data)
	if _, err := fmt.Fprintln(w, "listening on "+url); err != nil {
		return err
	}

	select {
	case err = <-served:
	case <-ctx.Done():
	}
	closeErr := runs.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), _shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		closeErr = errors.Join(closeErr, err)
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, closeErr)
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
