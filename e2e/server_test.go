package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// server is `atoll serve` running on a machine.
type server struct {
	m   *machine
	cmd *exec.Cmd
	url string
	log *bytes.Buffer
}

type (
	bot struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Persona  string `json:"persona"`
		Provider string `json:"provider"`
	}

	thread struct {
		ID    string `json:"id"`
		BotID string `json:"bot_id"`
	}

	message struct {
		ID    string `json:"id"`
		Role  string `json:"role"`
		Text  string `json:"text"`
		RunID string `json:"run_id"`
	}

	runInfo struct {
		ID       string  `json:"id"`
		ThreadID string  `json:"thread_id"`
		Status   string  `json:"status"`
		Reply    string  `json:"reply"`
		Error    string  `json:"error"`
		Report   *report `json:"report"`
	}

	report struct {
		Done    string `json:"done"`
		Tried   string `json:"tried"`
		Blocked string `json:"blocked"`
		Next    string `json:"next"`
	}

	// event is one SSE event of a thread, with when the test received it.
	event struct {
		Type string
		Data map[string]any
		At   time.Time
	}
)

// serve starts `atoll serve` on a free port with the machine's env. Cleanup kills it and every
// fake provider it started.
func (m *machine) serve(t *testing.T) *server {
	t.Helper()
	s := &server{m: m, log: &bytes.Buffer{}}
	s.cmd = exec.Command(_atoll, "serve", "--addr", "127.0.0.1:0")
	s.cmd.Dir = t.TempDir()
	s.cmd.WaitDelay = 2 * time.Second // a hung provider may hold atoll's stderr open
	s.cmd.Env = []string{
		"HOME=" + m.home,
		"PATH=" + m.path,
		"ATOLL_DATA_DIR=" + m.data,
		"ATOLL_PROVIDER_PATH=" + m.bin,
	}
	s.cmd.Stderr = s.log
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.kill()
		m.killFakes()
		if t.Failed() {
			t.Logf("atoll serve stderr:\n%s", s.log)
		}
	})

	first := make(chan string, 1)
	go func() {
		r := bufio.NewReader(stdout)
		line, _ := r.ReadString('\n')
		first <- line
		_, _ = io.Copy(io.Discard, r)
	}()
	select {
	case line := <-first:
		url, ok := strings.CutPrefix(strings.TrimSpace(line), "listening on ")
		if !ok || !strings.HasPrefix(url, "http://127.0.0.1:") {
			t.Fatalf("atoll serve: first line %q, want \"listening on http://127.0.0.1:<port>\"\nstderr:\n%s", line, s.log)
		}
		s.url = url
	case <-time.After(15 * time.Second):
		t.Fatalf("atoll serve printed nothing in 15s\nstderr:\n%s", s.log)
	}
	return s
}

// kill ends the server at once, as a crash would.
func (s *server) kill() {
	if s.cmd.ProcessState == nil {
		_ = s.cmd.Process.Signal(syscall.SIGKILL)
		_ = s.cmd.Wait()
	}
}

// killFakes kills every fake provider process the machine ran; a killed server leaves them.
func (m *machine) killFakes() {
	for _, p := range []string{"claude", "codex"} {
		b, _ := os.ReadFile(filepath.Join(m.bin, p+".log"))
		for line := range strings.Lines(string(b)) {
			var r struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal([]byte(line), &r) == nil && r.PID > 0 {
				_ = syscall.Kill(r.PID, syscall.SIGKILL)
			}
		}
	}
}

// do sends a JSON request and decodes a JSON reply into out; it fails the test on any other
// status than want.
func (s *server) do(t *testing.T, method, path string, body, out any, want int) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, s.url+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, resp.StatusCode, want, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: decode %s: %v", method, path, b, err)
		}
	}
}

func (s *server) newBot(t *testing.T, name, provider string) bot {
	t.Helper()
	var b bot
	s.do(t, "POST", "/api/bots", map[string]string{
		"name": name, "persona": "A careful Go engineer.", "provider": provider,
	}, &b, http.StatusCreated)
	if b.ID == "" || b.Name != name || b.Provider != provider {
		t.Fatalf("POST /api/bots returned %+v, want an id, name %q, provider %q", b, name, provider)
	}
	return b
}

func (s *server) newThread(t *testing.T, b bot) thread {
	t.Helper()
	var th thread
	s.do(t, "POST", "/api/bots/"+b.ID+"/threads", nil, &th, http.StatusCreated)
	if th.ID == "" || th.BotID != b.ID {
		t.Fatalf("POST /api/bots/%s/threads returned %+v, want an id and bot_id %q", b.ID, th, b.ID)
	}
	return th
}

// send posts a message to a thread and returns the run it started.
func (s *server) send(t *testing.T, th thread, text string) runInfo {
	t.Helper()
	var out struct {
		Message message `json:"message"`
		Run     runInfo `json:"run"`
	}
	s.do(t, "POST", "/api/threads/"+th.ID+"/messages", map[string]string{"text": text}, &out, http.StatusAccepted)
	if out.Run.ID == "" || out.Message.Text != text || out.Message.Role != "user" {
		t.Fatalf("POST /api/threads/%s/messages returned %+v, want the user message and a run", th.ID, out)
	}
	return out.Run
}

func (s *server) run(t *testing.T, id string) runInfo {
	t.Helper()
	var r runInfo
	s.do(t, "GET", "/api/runs/"+id, nil, &r, http.StatusOK)
	return r
}

// events subscribes to a thread's SSE stream. Subscribe before sending: past events are not
// replayed.
func (s *server) events(t *testing.T, th thread) <-chan event {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET", s.url+"/api/threads/"+th.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/threads/%s/events: %v", th.ID, err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("GET /api/threads/%s/events: status %d, Content-Type %q; want 200 text/event-stream",
			th.ID, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	ch := make(chan event, 256)
	go func() {
		defer func() { _ = resp.Body.Close() }()
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 16*1024*1024)
		var typ, data string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if data != "" {
					e := event{Type: typ, At: time.Now()}
					if typ == "" {
						e.Type = "message"
					}
					_ = json.Unmarshal([]byte(data), &e.Data)
					ch <- e
				}
				typ, data = "", ""
			case strings.HasPrefix(line, "event:"):
				typ = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data += strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			}
		}
	}()
	return ch
}

// until returns the events of run up to and including the first of type typ. It fails on an
// approval event unless typ is "approval": Atoll's own tools must never ask the user.
func until(t *testing.T, ch <-chan event, runID, typ string) []event {
	t.Helper()
	var seen []event
	timeout := time.After(30 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("event stream closed before %s of run %s; got %s", typ, runID, types(seen))
			}
			if e.Data["run_id"] != runID {
				continue
			}
			seen = append(seen, e)
			if e.Type == typ {
				return seen
			}
			if e.Type == "approval" {
				t.Fatalf("run %s asked for approval before %s: %v", runID, typ, e.Data)
			}
			if e.Type == "run_finished" {
				t.Fatalf("run %s finished before %s: %v; events %s", runID, typ, e.Data, types(seen))
			}
		case <-timeout:
			t.Fatalf("no %s event of run %s in 30s; got %s", typ, runID, types(seen))
		}
	}
}

func types(es []event) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Type)
	}
	return "[" + strings.Join(out, " ") + "]"
}

// finished waits for the run_finished event of run and returns the run as the API has it.
func (s *server) finished(t *testing.T, ch <-chan event, runID string) runInfo {
	t.Helper()
	until(t, ch, runID, "run_finished")
	return s.run(t, runID)
}
