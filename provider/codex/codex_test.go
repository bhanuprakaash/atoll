package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atoll/provider"
)

const (
	_threadID = "01a1a5b5-3b2c-71f2-84d9-e7c08190f545"
	_fact     = "Remember this fact: the codeword is heron. Reply with just: ok"
	_recall   = "What is the codeword? Reply with just the word."
	_skills   = "List your skills and MCP servers. Name every one you have; if you have none of a kind, say none."
	_write    = "Use the Write tool to create hello.txt containing the word hi."
)

func TestSession(t *testing.T) {
	testdata, err := filepath.Abs("../../e2e/testdata")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "codex")
	build := exec.Command("go", "build", "-o", bin,
		"-ldflags", "-X atoll/e2e/fakes/fake._testdata="+testdata, "atoll/e2e/fakes/codex")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake codex: %v\n%s", err, out)
	}
	t.Setenv("HOME", t.TempDir())

	tests := []struct {
		name        string
		resume      string
		prompt      string
		wantReply   string
		wantSession string
		wantErr     string
		mcpURL      string
		allow       bool
		wantCalls   string // names of the KindToolCall events, joined by ","
		wantAsk     string // name and ID of the KindApproval event, if any
	}{
		{
			name:        "new thread",
			prompt:      _fact,
			wantReply:   "ok",
			wantSession: _threadID,
		},
		{
			name:        "resume recalls",
			resume:      _threadID,
			prompt:      _recall,
			wantReply:   "heron",
			wantSession: _threadID,
		},
		{
			name:        "commentary is left out of the reply",
			prompt:      _skills,
			wantReply:   "- Skills: none.\n- MCP servers: none connected or exposed.",
			wantSession: _threadID,
			wantCalls:   "list_mcp_resource_templates,list_mcp_resources",
		},
		{
			name:        "approval accepted",
			prompt:      _write,
			mcpURL:      "http://127.0.0.1:1/mcp",
			allow:       true,
			wantReply:   "Created hello.txt containing hi.",
			wantSession: _threadID,
			wantCalls:   "commandExecution,finish_run",
			wantAsk:     "commandExecution 0",
		},
		{
			name:        "approval declined",
			prompt:      _write,
			mcpURL:      "http://127.0.0.1:1/mcp",
			wantReply:   "I didn't create hello.txt because you declined the command.",
			wantSession: _threadID,
			wantCalls:   "commandExecution,finish_run",
			wantAsk:     "commandExecution 0",
		},
		{
			name:    "resume unknown id",
			resume:  "no-such-thread",
			prompt:  _recall,
			wantErr: "no rollout found for thread id no-such-thread",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cfg := provider.Config{Bin: bin, Dir: t.TempDir(), Cwd: t.TempDir(), MCPURL: tt.mcpURL}
			s := New()
			defer func() { _ = s.Interrupt() }()
			if tt.resume != "" {
				err = s.Resume(ctx, cfg, tt.resume)
			} else {
				err = s.Start(ctx, cfg)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("start error %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Send(ctx, tt.prompt, nil); err != nil {
				t.Fatal(err)
			}
			var reply strings.Builder
			session, calls, ask := "", []string{}, ""
			for ev := range s.Events() {
				switch ev.Kind {
				case provider.KindToolCall:
					calls = append(calls, ev.Name)
				case provider.KindApproval:
					ask = ev.Name + " " + ev.ID
					if err := s.Answer(ev.ID, tt.allow); err != nil {
						t.Fatal(err)
					}
					if s.Answer(ev.ID, tt.allow) == nil {
						t.Error("a second answer to the same request was accepted")
					}
				case provider.KindSession:
					session = ev.SessionID
				case provider.KindText:
					reply.WriteString(ev.Text)
				case provider.KindError:
					t.Fatalf("error event: %s", ev.Text)
				}
				if ev.Kind == provider.KindTurnEnd {
					break
				}
			}
			if reply.String() != tt.wantReply || session != tt.wantSession ||
				strings.Join(calls, ",") != tt.wantCalls || ask != tt.wantAsk {
				t.Errorf("got reply %q session %q calls %q ask %q, want %q %q %q %q",
					reply.String(), session, strings.Join(calls, ","), ask,
					tt.wantReply, tt.wantSession, tt.wantCalls, tt.wantAsk)
			}
		})
	}
}

func TestSession_handleText(t *testing.T) {
	started := func(id, phase string) string {
		item := `{"type":"agentMessage","id":"` + id + `"`
		if phase != "" {
			item += `,"phase":"` + phase + `"`
		}
		return `{"method":"item/started","params":{"item":` + item + `}}}`
	}
	delta := func(id, text string) string {
		return `{"method":"item/agentMessage/delta","params":{"itemId":"` + id + `","delta":"` + text + `"}}`
	}
	turn := `{"method":"turn/started","params":{}}`
	tests := []struct {
		name string
		give []string
		want string
	}{
		{
			name: "commentary then answer",
			give: []string{
				turn,
				started("a", "commentary"), delta("a", "I'll remember."),
				started("b", "final_answer"), delta("b", "ok"),
			},
			want: "ok",
		},
		{
			name: "answer without a phase",
			give: []string{turn, started("a", ""), delta("a", "ok")},
			want: "ok",
		},
		{
			name: "two answers are split",
			give: []string{
				turn,
				started("a", "final_answer"), delta("a", "one"),
				started("b", "commentary"), delta("b", "note"),
				started("c", "final_answer"), delta("c", "two"),
			},
			want: "one\n\ntwo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			var reply strings.Builder
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				for ev := range s.events {
					if ev.Kind == provider.KindText {
						reply.WriteString(ev.Text)
					}
				}
			}()
			for _, line := range tt.give {
				var m message
				if err := json.Unmarshal([]byte(line), &m); err != nil {
					t.Fatal(err)
				}
				if !s.handle(m) {
					t.Fatal("handle stopped reading")
				}
			}
			close(s.events)
			<-drained
			if reply.String() != tt.want {
				t.Errorf("reply %q, want %q", reply.String(), tt.want)
			}
		})
	}
}

func TestThreadConfig(t *testing.T) {
	tests := []struct {
		name   string
		skills []string
		want   string
	}{
		{
			name: "no skills folder",
			want: `{"features":{"apps":false,"plugins":false,"remote_plugin":false},` +
				`"skills":{"bundled":{"enabled":false}}}`,
		},
		{
			name:   "each skill folder is disabled",
			skills: []string{"alpha", "beta"},
			want: `{"features":{"apps":false,"plugins":false,"remote_plugin":false},` +
				`"skills":{"bundled":{"enabled":false},"config":[` +
				`{"enabled":false,"path":"HOME/.agents/skills/alpha/SKILL.md"},` +
				`{"enabled":false,"path":"HOME/.agents/skills/beta/SKILL.md"}]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			for _, name := range tt.skills {
				if err := os.MkdirAll(filepath.Join(home, ".agents", "skills", name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := threadConfig(home)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.ReplaceAll(tt.want, "HOME", home); string(got) != want {
				t.Errorf("threadConfig = %s, want %s", got, want)
			}
		})
	}
}

func TestThreadParams(t *testing.T) {
	tests := []struct {
		name   string
		mcpURL string
		system string
		want   string
	}{
		{
			name: "without MCP",
			want: `{"approvalPolicy":"never","config":{"features":{"apps":false,"plugins":false,` +
				`"remote_plugin":false},"skills":{"bundled":{"enabled":false}}},"cwd":"/work"}`,
		},
		{
			name:   "with MCP",
			mcpURL: "http://127.0.0.1:9/mcp/t0k",
			want: `{"approvalPolicy":"on-request","config":{"features":{"apps":false,"plugins":false,` +
				`"remote_plugin":false},"mcp_servers":{"atoll":{"url":"http://127.0.0.1:9/mcp/t0k"}},` +
				`"skills":{"bundled":{"enabled":false}}},"cwd":"/work","sandbox":"workspace-write"}`,
		},
		{name: "system addition", system: "addition", want: `{"approvalPolicy":"never","config":{"features":{"apps":false,"plugins":false,` +
			`"remote_plugin":false},"skills":{"bundled":{"enabled":false}}},"cwd":"/work","developerInstructions":"addition"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			params, err := threadParams(provider.Config{Cwd: "/work", MCPURL: tt.mcpURL, SystemPrompt: tt.system})
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("threadParams = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFileChangeApprovalDetails(t *testing.T) {
	tests := []struct{ name, itemID, want string }{
		{name: "matching item", itemID: "edit-1", want: `"changes":[{"path":"a.go","diff":"+line"}]`},
		{name: "unmatched item", itemID: "other", want: `"itemId":"other"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.handle(message{Method: "item/started", Params: json.RawMessage(`{"item":{"type":"fileChange","id":"edit-1","changes":[{"path":"a.go","diff":"+line"}]}}`)})
			<-s.Events()
			s.serverRequest(message{ID: json.RawMessage(`1`), Method: "item/fileChange/requestApproval", Params: json.RawMessage(`{"itemId":"` + tt.itemID + `"}`)})
			ev := <-s.Events()
			if !strings.Contains(string(ev.Input), tt.want) {
				t.Errorf("approval input = %s, want %s", ev.Input, tt.want)
			}
		})
	}
}
