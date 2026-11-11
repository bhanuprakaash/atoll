package e2e

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Slice 6: these prove the new fake behaviour works with no product: model lists, sign-in, and
// the paced working scenario. fresh-fix reuses the repo-fix directive and run-approve's ask,
// both proven by earlier fixture tests.

const (
	_workingPrompt  = "Fix the wizard repo select and run its tests."
	_workingReply   = "Fixed the wizard repo select; tests pass."
	_claudeLoginURL = "https://login.reef.test/claude/oauth?code=true"
	_codexLoginURL  = "https://login.reef.test/codex/oauth?client_id=fake"
	_workingPause   = time.Second // each tool call of the working scenario takes this long
)

var _probeModel = map[string]string{"claude": "reef-probe", "codex": "reef-probe-codex"}

// fakeCmd is the fake provider of m run with Atoll's settings folders, as the product runs it.
func fakeCmd(t *testing.T, m *machine, p string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), filepath.Join(m.bin, p), args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"HOME=" + m.home, "PATH=" + m.path, "CLAUDE_CONFIG_DIR=" + m.providerDir("claude"), "CODEX_HOME=" + m.providerDir("codex")}
	return cmd
}

func TestMacAppFixtures_Models(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			var cmd *exec.Cmd
			var input string
			if p == "claude" {
				cmd = fakeCmd(t, m, p, "-p", "--input-format", "stream-json", "--output-format", "stream-json")
				input = `{"type":"control_request","request_id":"r1","request":{"subtype":"initialize"}}` + "\n"
			} else {
				cmd = fakeCmd(t, m, p, "app-server")
				input = `{"id":1,"method":"initialize","params":{}}` + "\n" + `{"id":2,"method":"model/list","params":{}}` + "\n"
			}
			cmd.Stdin = strings.NewReader(input)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("fake %s: %v\n%s", p, err, out)
			}
			if !strings.Contains(string(out), `"`+_probeModel[p]+`"`) {
				t.Errorf("fake %s model list %s, want %s in it", p, out, _probeModel[p])
			}
		})
	}
}

func TestMacAppFixtures_Login(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			args := map[string][]string{"claude": {"auth", "login"}, "codex": {"login"}}[p]
			cred := filepath.Join(m.providerDir(p), map[string]string{"claude": ".credentials.json", "codex": "auth.json"}[p])
			cmd := fakeCmd(t, m, p, args...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			url := map[string]string{"claude": _claudeLoginURL, "codex": _codexLoginURL}[p]
			r := bufio.NewReader(stdout)
			for {
				line, err := r.ReadString('\n')
				if strings.Contains(line, url) {
					break
				}
				if err != nil {
					t.Fatalf("fake %s login ended before printing %s", p, url)
				}
			}
			time.Sleep(200 * time.Millisecond)
			if _, err := os.Stat(cred); err == nil {
				t.Fatalf("fake %s login wrote %s before release", p, cred)
			}
			writeFile(t, filepath.Join(m.bin, p+".login.release"), "")
			_, _ = io.Copy(io.Discard, r)
			if err := cmd.Wait(); err != nil {
				t.Fatalf("fake %s login: %v", p, err)
			}
			if _, err := os.Stat(cred); err != nil {
				t.Errorf("fake %s login did not write %s: %v", p, cred, err)
			}
		})
	}
}

// stubMCP is an MCP server that answers every tool call with "Report saved.".
func stubMCP(t *testing.T) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "stub", "version": "0"}}
		case "tools/list":
			result = map[string]any{"tools": []any{}}
		case "tools/call":
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": "Report saved."}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(s.Close)
	return s.URL
}

func TestMacAppFixtures_Working(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			start := time.Now()
			out := runSkillFake(t, m, p, t.TempDir(), t.TempDir(), _workingPrompt, stubMCP(t), nil)
			if took := time.Since(start); took < 4*_workingPause {
				t.Errorf("working scenario took %v, want at least %v: each of its 4 tool calls pauses", took, 4*_workingPause)
			}
			var want []string
			if p == "claude" {
				want = []string{`"name":"Grep"`, `"name":"Read"`, `"name":"Edit"`, `"name":"Bash"`}
			} else {
				want = []string{`"type":"search"`, `"type":"read"`, `"type":"fileChange"`, `"command":"pnpm test"`}
			}
			at := 0
			for _, w := range want {
				i := strings.Index(out[at:], w)
				if i < 0 {
					t.Fatalf("fake %s working output lacks %s after offset %d:\n%s", p, w, at, out)
				}
				at += i
			}
			if !strings.Contains(out, _workingReply) {
				t.Errorf("fake %s working output lacks the reply %q", p, _workingReply)
			}
		})
	}
}
