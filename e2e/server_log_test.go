package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Contract: sign-in-lost-contract.md.

// serverLogText is <data dir>/logs/server.log; it fails the test when the file is missing.
func serverLogText(t *testing.T, m *machine) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(m.data, "logs", "server.log"))
	if err != nil {
		t.Fatalf("read the server log file: %v", err)
	}
	return string(b)
}

// jsonLines are the lines of text that are JSON objects, trimmed.
func jsonLines(text string) []string {
	var out []string
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) == nil {
			out = append(out, line)
		}
	}
	return out
}

// signInLostLines are the `GitHub sign-in lost` lines of the server log file.
func signInLostLines(t *testing.T, m *machine) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range jsonLines(serverLogText(t, m)) {
		var v map[string]any
		_ = json.Unmarshal([]byte(line), &v)
		if v["msg"] == "GitHub sign-in lost" {
			out = append(out, v)
		}
	}
	return out
}

// wantSignInLost checks the one `GitHub sign-in lost` line in the log file, and that no token the
// fake GitHub issued is in the file.
func wantSignInLost(t *testing.T, m *machine, gh *fakeGitHub, connID, botID, endpointPrefix string, exact bool) {
	t.Helper()
	lines := signInLostLines(t, m)
	if len(lines) != 1 {
		t.Fatalf("server log has %d `GitHub sign-in lost` lines, want 1: %v", len(lines), lines)
	}
	l := lines[0]
	if l["level"] != "WARN" {
		t.Errorf("sign-in lost line level %v, want WARN: %v", l["level"], l)
	}
	if l["connection_id"] != connID || l["bot_id"] != botID {
		t.Errorf("sign-in lost line connection_id %v bot_id %v, want %s and %s: %v", l["connection_id"], l["bot_id"], connID, botID, l)
	}
	endpoint, _ := l["endpoint"].(string)
	if exact && endpoint != endpointPrefix || !exact && !strings.HasPrefix(endpoint, endpointPrefix) {
		t.Errorf("sign-in lost line endpoint %q, want %q (exact %v)", endpoint, endpointPrefix, exact)
	}
	if l["github_message"] != "Bad credentials" {
		t.Errorf("sign-in lost line github_message %v, want \"Bad credentials\"", l["github_message"])
	}
	if l["github_request_id"] != _fakeGitHubRequestID {
		t.Errorf("sign-in lost line github_request_id %v, want %q", l["github_request_id"], _fakeGitHubRequestID)
	}
	text := serverLogText(t, m)
	gh.mu.Lock()
	issued, tag := gh.issued, gh.tag
	gh.mu.Unlock()
	for i := 1; i <= issued; i++ {
		if tok := fmt.Sprintf("fake-gho-%s-%d", tag, i); strings.Contains(text, tok) {
			t.Errorf("server log holds the token %q", tok)
		}
	}
}

// connectionJSON is the connection as GET /api/connections and GET /api/connections/{id} give it;
// it fails the test when the two differ in status or signed_out_at.
func connectionJSON(t *testing.T, s *server, id string) map[string]any {
	t.Helper()
	var list []map[string]any
	s.do(t, "GET", "/api/connections", nil, &list, 200)
	var fromList map[string]any
	for _, c := range list {
		if c["id"] == id {
			fromList = c
		}
	}
	if fromList == nil {
		t.Fatalf("GET /api/connections has no connection %s: %v", id, list)
	}
	var one map[string]any
	s.do(t, "GET", "/api/connections/"+id, nil, &one, 200)
	if fromList["status"] != one["status"] || fromList["signed_out_at"] != one["signed_out_at"] {
		t.Fatalf("GET /api/connections %v and GET /api/connections/%s %v differ in status or signed_out_at", fromList, id, one)
	}
	return one
}

// wantSignedOutAt checks that the connection is needs_sign_in with a signed_out_at in UTC between
// from and to (to the second).
func wantSignedOutAt(t *testing.T, s *server, id string, from, to time.Time) {
	t.Helper()
	c := connectionJSON(t, s, id)
	raw, ok := c["signed_out_at"].(string)
	if c["status"] != "needs_sign_in" || !ok {
		t.Errorf("connection %v, want needs_sign_in with signed_out_at", c)
		return
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil || !strings.HasSuffix(raw, "Z") {
		t.Errorf("signed_out_at %q, want RFC 3339 in UTC (err %v)", raw, err)
		return
	}
	if at.Before(from.Truncate(time.Second)) || at.After(to.Add(time.Second)) {
		t.Errorf("signed_out_at %s, want between %s and %s", at, from.UTC(), to.UTC())
	}
}

func TestServerLog_MirrorsStderrAndAppends(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	s := m.serve(t)
	s.newBot(t, "Logger", "claude")
	s.kill()
	first := jsonLines(s.log.String())
	if len(first) == 0 {
		t.Fatalf("atoll serve wrote no JSON log line to stderr:\n%s", s.log)
	}
	file := jsonLines(serverLogText(t, m))
	for _, line := range first {
		if !slices.Contains(file, line) {
			t.Errorf("stderr line missing from the server log file: %s", line)
		}
	}

	s = m.serve(t)
	s.kill()
	second := jsonLines(s.log.String())
	after := jsonLines(serverLogText(t, m))
	if len(after) < len(file) || !slices.Equal(after[:len(file)], file) {
		t.Errorf("server log file after a restart does not start with the lines from before: %d lines before, %d after", len(file), len(after))
	}
	for _, line := range second {
		if !slices.Contains(after, line) {
			t.Errorf("stderr line of the second start missing from the server log file: %s", line)
		}
	}
}
