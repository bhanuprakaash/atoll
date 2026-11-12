package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// tokenPipe returns the number of a pipe fd that holds text and then ends.
func tokenPipe(t *testing.T, text string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(text); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	fd, err := syscall.Dup(int(r.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return strconv.Itoa(fd)
}

func TestScrubEnv_TokenStaysOutOfChildren(t *testing.T) {
	t.Setenv("ATOLL_TOKEN", "")
	_ = os.Unsetenv("ATOLL_TOKEN")
	t.Setenv(_tokenFD, tokenPipe(t, "api-credential"))
	got, err := scrubEnv()
	if err != nil || got != "api-credential" {
		t.Fatalf("scrubEnv = %q, %v; want the token", got, err)
	}
	out, err := exec.CommandContext(t.Context(), "env").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ATOLL_TOKEN") || strings.Contains(string(out), "api-credential") {
		t.Errorf("a child process inherited the API token:\n%s", out)
	}
}

func TestScrubEnv_BadPipe(t *testing.T) {
	tests := []struct {
		name string
		fd   string
	}{
		{"not a number", "x"},
		{"closed descriptor", "9999"},
		{"empty pipe", tokenPipe(t, "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(_tokenFD, tt.fd)
			if got, err := scrubEnv(); err == nil {
				t.Errorf("scrubEnv = %q, nil; want an error, not a server without auth", got)
			}
		})
	}
}
