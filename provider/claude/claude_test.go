package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"atoll/provider"
)

const (
	_factPrompt   = "Remember this fact: the codeword is heron. Reply with just: ok"
	_recallPrompt = "What is the codeword? Reply with just the word."
	_factID       = "567fd9ca-fc18-4a32-a242-a6e148914e80"
	_skillsPrompt = "List your skills and MCP servers. Name every one you have; " +
		"if you have none of a kind, say none."
	_skillsID    = "d662d248-7813-41a1-b832-28f02d5f27aa"
	_skillsReply = "**Skills (15):**\n- dataviz\n- update-config\n- keybindings-help\n- code-review\n" +
		"- simplify\n- fewer-permission-prompts\n- loop\n- claude-api\n- workflow-authoring\n- run\n" +
		"- plugin-authoring\n- init\n- security-review\n\n" +
		"That's 13 names. The system listing also includes two more I haven't counted yet, " +
		"so I'll correct that: the list has 13 entries in total.\n\n" +
		"**MCP servers:** none. I have no `mcp__*` tools loaded, and the deferred tools " +
		"(CronCreate, DesignSync, WebFetch, WebSearch, and others) are built-in harness tools, " +
		"not MCP servers."
)

func TestSession(t *testing.T) {
	testdata, err := filepath.Abs("../../e2e/testdata")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "claude")
	build := exec.Command("go", "build", "-o", bin,
		"-ldflags", "-X atoll/e2e/fakes/fake._testdata="+testdata, "atoll/e2e/fakes/claude")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake claude: %v\n%s", err, out)
	}

	tests := []struct {
		name        string
		resume      string
		prompt      string
		wantText    string
		wantSession string
		wantEnd     provider.Kind
	}{
		{"new session", "", _factPrompt, "ok", _factID, provider.KindTurnEnd},
		{"resume recalls", _factID, _recallPrompt, "heron", _factID, provider.KindTurnEnd},
		{"reply in many deltas", "", _skillsPrompt, _skillsReply, _skillsID, provider.KindTurnEnd},
		{"resume unknown id", "00000000-0000-4000-8000-000000000000", _recallPrompt, "", "", provider.KindError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cfg := provider.Config{Bin: bin, Dir: t.TempDir(), Cwd: t.TempDir()}
			s := New()
			var err error
			if tt.resume == "" {
				err = s.Start(ctx, cfg)
			} else {
				err = s.Resume(ctx, cfg, tt.resume)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Interrupt() })
			if err := s.Send(ctx, tt.prompt); err != nil {
				t.Fatal(err)
			}

			var text strings.Builder
			var session string
			var end provider.Kind
			for end == 0 {
				select {
				case ev, ok := <-s.Events():
					if !ok {
						t.Fatalf("events closed before the turn ended; text %q", text.String())
					}
					switch ev.Kind {
					case provider.KindText:
						text.WriteString(ev.Text)
					case provider.KindSession:
						session = ev.SessionID
					case provider.KindTurnEnd, provider.KindError:
						end = ev.Kind
					}
				case <-ctx.Done():
					t.Fatalf("no turn end: %v", ctx.Err())
				}
			}

			if text.String() != tt.wantText || session != tt.wantSession || end != tt.wantEnd {
				t.Errorf("got text %q, session %q, end %d; want %q, %q, %d",
					text.String(), session, end, tt.wantText, tt.wantSession, tt.wantEnd)
			}
		})
	}
}

func TestArgv(t *testing.T) {
	base := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages", "--strict-mcp-config", "--settings", `{"disableAllHooks":true,"disableBundledSkills":true,"skillOverrides":{"design":"off","doctor":"off","plugin-authoring":"off"}}`,
	}
	mcp := []string{
		"--mcp-config", `{"mcpServers":{"atoll":{"type":"http","url":"http://127.0.0.1:9/mcp/t0k"}}}`,
		"--permission-prompt-tool", "mcp__atoll__approve",
		"--allowedTools", "mcp__atoll",
	}
	tests := []struct {
		name   string
		mcpURL string
		system string
		extra  []string
		want   []string
	}{
		{"without MCP", "", "", nil, base},
		{"with MCP", "http://127.0.0.1:9/mcp/t0k", "", nil, append(base[:len(base):len(base)], mcp...)},
		{"resume comes last", "http://127.0.0.1:9/mcp/t0k", "", []string{"--resume", "abc"},
			append(append(base[:len(base):len(base)], mcp...), "--resume", "abc")},
		{"system on resume", "", "addition", []string{"--resume", "abc"}, append(base[:len(base):len(base)], "--append-system-prompt", "addition", "--resume", "abc")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := argv(provider.Config{MCPURL: tt.mcpURL, SystemPrompt: tt.system}, tt.extra...)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argv = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []provider.Event
	}{
		{
			name: "assistant tool calls, text skipped",
			line: `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"},` +
				`{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}},` +
				`{"type":"tool_use","id":"toolu_2","name":"mcp__atoll__finish_run","input":{"done":"x"}}]}}`,
			want: []provider.Event{
				{Kind: provider.KindToolCall, ID: "toolu_1", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`)},
				{Kind: provider.KindToolCall, ID: "toolu_2", Name: "mcp__atoll__finish_run", Input: json.RawMessage(`{"done":"x"}`)},
			},
		},
		{
			name: "partial tool input is not a call",
			line: `{"type":"stream_event","event":{"type":"content_block_start",` +
				`"content_block":{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parse([]byte(tt.line), &state{}); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parse = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAdoptSession(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "projects", "-data-threads-t1")
	if err := os.MkdirAll(from, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "s1.jsonl"), []byte("history"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := provider.Config{Dir: dir, Cwd: "/data/trees/seeded/fix-seeded.bug"}
	if err := adoptSession(cfg, "s1"); err != nil {
		t.Fatal(err)
	}

	to := filepath.Join(dir, "projects", "-data-trees-seeded-fix-seeded-bug", "s1.jsonl")
	if got, err := os.ReadFile(to); err != nil || string(got) != "history" {
		t.Fatalf("copied session = %q, %v; want %q", got, err, "history")
	}
}
