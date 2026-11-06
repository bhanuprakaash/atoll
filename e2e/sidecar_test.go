package e2e

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServe_ExitsWhenStdinCloses(t *testing.T) {
	m := newMachine(t)
	addr := freeAddr(t)
	_, stdin, done, stderr := serveCommand(t, m, addr, "--exit-on-stdin-close")

	waitForHTTP(t, done, stderr, addr)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("atoll serve after stdin closed: %v\nstderr:\n%s", err, stderr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("atoll serve still running 3s after stdin closed\nstderr:\n%s", stderr)
	}
}

func TestServe_ReportsAddressAlreadyInUse(t *testing.T) {
	m := newMachine(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	addr := listener.Addr().String()
	_, _, done, stderr := serveCommand(t, m, addr)

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("atoll serve on occupied address %s exited successfully\nstderr:\n%s", addr, stderr)
		}
		want := addr + " is in use by another program"
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("atoll serve on occupied address stderr %q, want phrase %q", stderr.String(), want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("atoll serve did not exit within 3s for occupied address %s\nstderr:\n%s", addr, stderr)
	}
}

func TestServe_SIGTERMLeavesNoRunningRunAfterRestart(t *testing.T) {
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	events := s.events(t, th)
	run := s.send(t, th, _hangPrompt)
	until(t, events, run.ID, "tool_call")

	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("atoll serve after SIGTERM: %v\nstderr:\n%s", err, s.log)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("atoll serve did not stop cleanly within 3s after SIGTERM")
	}

	s = m.serve(t)
	if got := s.run(t, run.ID); got.Status == "running" {
		t.Fatalf("run after SIGTERM and restart: status %q, want not running", got.Status)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func serveCommand(t *testing.T, m *machine, addr string, extra ...string) (*exec.Cmd, io.WriteCloser, <-chan error, *bytes.Buffer) {
	t.Helper()
	args := append([]string{"serve", "--addr", addr}, extra...)
	cmd := exec.Command(_atoll, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"HOME=" + m.home,
		"PATH=" + m.path,
		"ATOLL_DATA_DIR=" + m.data,
		"ATOLL_PROVIDER_PATH=" + m.bin,
	}
	cmd.Env = append(cmd.Env, m.env...)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Signal(syscall.SIGKILL)
			_ = <-done
		}
	})
	return cmd, stdin, done, stderr
}

func waitForHTTP(t *testing.T, done <-chan error, stderr *bytes.Buffer, addr string) {
	t.Helper()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.After(3 * time.Second)
	for {
		response, err := client.Get("http://" + addr + "/api/bots")
		if err == nil {
			_ = response.Body.Close()
			return
		}
		select {
		case waitErr := <-done:
			t.Fatalf("atoll serve exited before answering HTTP: %v\nstderr:\n%s", waitErr, stderr)
		case <-deadline:
			t.Fatalf("atoll serve did not answer HTTP within 3s\nstderr:\n%s", stderr)
		case <-time.After(25 * time.Millisecond):
		}
	}
}
