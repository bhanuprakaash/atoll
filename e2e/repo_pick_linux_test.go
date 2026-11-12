//go:build linux

package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #227: on Linux the repo folder picker is zenity, run by the server, so the UI still talks
// to Go over HTTP only.
func TestRepos_PickFolderLinux(t *testing.T) {
	t.Parallel()
	type picked struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	// zenity answers like the real one: the folder on stdout and exit 0, or exit 1 on Cancel. It
	// logs its arguments next to itself.
	zenity := func(t *testing.T, m *machine, body string) string {
		t.Helper()
		writeExec(t, filepath.Join(m.bin, "zenity"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$(dirname \"$0\")/zenity.args\"\n"+body)
		return filepath.Join(m.bin, "zenity.args")
	}

	t.Run("chosen", func(t *testing.T) {
		m := newMachine(t, "claude")
		args := zenity(t, m, "echo /home/ada/src/reef\n")
		s := m.serve(t)
		var got picked
		s.do(t, http.MethodPost, "/api/repos/pick-folder", nil, &got, http.StatusOK)
		if got.Path != "/home/ada/src/reef" {
			t.Errorf("path = %q, want /home/ada/src/reef", got.Path)
		}
		b, err := os.ReadFile(args)
		if err != nil {
			t.Fatalf("zenity did not run: %v", err)
		}
		if a := strings.Fields(string(b)); len(a) < 2 || a[0] != "--file-selection" || a[1] != "--directory" {
			t.Errorf("zenity args = %q, want --file-selection --directory first", a)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		m := newMachine(t, "claude")
		zenity(t, m, "exit 1\n")
		s := m.serve(t)
		s.do(t, http.MethodPost, "/api/repos/pick-folder", nil, nil, http.StatusNoContent)
	})

	t.Run("no zenity", func(t *testing.T) {
		m := newMachine(t, "claude")
		m.path = m.bin
		s := m.serve(t)
		var got struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		s.do(t, http.MethodPost, "/api/repos/pick-folder", nil, &got, http.StatusNotImplemented)
		if got.Error != "No folder picker on this system." || got.Code != "unsupported" {
			t.Errorf("reply {error: %q, code: %q}, want {No folder picker on this system., unsupported}", got.Error, got.Code)
		}
	})
}
