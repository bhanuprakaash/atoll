package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Contract: secret-store-down-contract.md.

const _secretStoreSentence = "Reef can't reach its secret store. Restart Reef, or unlock it, then send again."

// storeDown restarts the machine's server with the secret store unreachable, on the same data.
func storeDown(t *testing.T, m *machine, s *server) *server {
	t.Helper()
	s.kill()
	m.env = append(m.env, "ATOLL_SECRET_STORE_UNAVAILABLE=1")
	return m.serve(t)
}

type secretStoreRun struct {
	Status    string `json:"status"`
	Error     string `json:"error"`
	ErrorKind string `json:"error_kind"`
}

// warnLines are the WARN lines of the server log that name runID.
func warnLines(t *testing.T, log *bytes.Buffer, runID string) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(log.Bytes()))
	for sc.Scan() {
		var line map[string]any
		if json.Unmarshal(sc.Bytes(), &line) == nil && line["level"] == "WARN" && line["run_id"] == runID {
			out = append(out, line)
		}
	}
	return out
}

func TestSecretStoreDown_RunFailsPlainlyAndKeepsTheMessage(t *testing.T) {
	t.Parallel()
	type setup func(t *testing.T, p string) (*machine, *server, bot)
	for _, tc := range []struct {
		name  string
		setup setup
	}{
		{"github connection", func(t *testing.T, p string) (*machine, *server, bot) {
			f := newGitHubFixture(t, p)
			return f.m, f.s, f.bot(t, p, []string{f.connect(t)})
		}},
		{"mcp env secret", func(t *testing.T, p string) (*machine, *server, bot) {
			f := newMCPFixture(t, p)
			return f.m, f.s, f.bot(t, "Docs", f.local.ID)
		}},
	} {
		for _, p := range _providers {
			t.Run(tc.name+"/"+p, func(t *testing.T) {
				m, s, b := tc.setup(t, p)
				s = storeDown(t, m, s)

				th := s.newThread(t, b)
				ch := s.events(t, th)
				r := s.send(t, th, _hey)
				until(t, ch, r.ID, "run_finished")
				var run secretStoreRun
				s.do(t, "GET", "/api/runs/"+r.ID, nil, &run, http.StatusOK)
				if run.Status != "failed" || run.ErrorKind != "secret_store" || run.Error != _secretStoreSentence {
					t.Errorf("run with the store down: %+v, want failed, error_kind secret_store, error %q", run, _secretStoreSentence)
				}
				if got := len(m.sessions(t, p)); got != 0 {
					t.Errorf("%s started %d times with the store down, want 0", p, got)
				}

				var msgs []struct {
					Role    string `json:"role"`
					Text    string `json:"text"`
					RunID   string `json:"run_id"`
					NotRead bool   `json:"not_read"`
				}
				s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &msgs, http.StatusOK)
				if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Text != _hey || msgs[0].RunID != r.ID || !msgs[0].NotRead {
					t.Errorf("thread messages %+v, want the user message %q kept with not_read true", msgs, _hey)
				}

				s.kill()
				warns := warnLines(t, s.log, r.ID)
				if len(warns) != 1 || warns[0]["error_kind"] != "secret_store" {
					t.Errorf("WARN lines for run %s: %v, want one with error_kind secret_store\nlog:\n%s", r.ID, warns, s.log)
				}
			})
		}
	}
}

func TestSecretStoreDown_RunWithoutSecretsStillWorks(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubFixture(t, p)
			f.connect(t)
			plain := f.s.newBot(t, "Plain", p)
			s := storeDown(t, f.m, f.s)

			th := s.newThread(t, plain)
			ch := s.events(t, th)
			run := s.finished(t, ch, s.send(t, th, _hey).ID)
			if run.Status != "done" || strings.TrimSpace(run.Reply) == "" {
				t.Errorf("run of a Bot with no connection, store down: %+v, want done with a reply", run)
			}
		})
	}
}
