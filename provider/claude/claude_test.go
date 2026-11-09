package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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
			if err := s.Send(ctx, tt.prompt, nil); err != nil {
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
		"--include-partial-messages", "--strict-mcp-config", "--setting-sources", "project", "--settings", `{"autoMemoryEnabled":false,"claudeMdExcludes":["*.md",".claude/**"],"disableAllHooks":true,"disableBundledSkills":true,"skillOverrides":{"design":"off","doctor":"off","plugin-authoring":"off"}}`, "--tools", "Bash,Edit,Glob,Grep,NotebookEdit,Read,Skill,WebFetch,WebSearch,Write",
	}
	noTools := slices.Clone(base)
	noTools[len(noTools)-1] = ""
	mcp := func(servers string) []string {
		return []string{
			"--mcp-config", servers,
			"--permission-prompt-tool", "mcp__atoll__approve",
			"--allowedTools", "mcp__atoll",
		}
	}
	atoll := mcp(`{"mcpServers":{"atoll":{"type":"http","url":"${ATOLL_MCP_0}"}}}`)
	atollSecrets := []string{"ATOLL_MCP_0=http://127.0.0.1:9/mcp/t0k"}
	tests := []struct {
		name        string
		cfg         provider.Config
		extra       []string
		want        []string
		wantSecrets []string
	}{
		{"without MCP", provider.Config{}, nil, base, nil},
		{"with MCP", provider.Config{MCPURL: "http://127.0.0.1:9/mcp/t0k"}, nil,
			append(base[:len(base):len(base)], atoll...), atollSecrets},
		{"resume comes last", provider.Config{MCPURL: "http://127.0.0.1:9/mcp/t0k"}, []string{"--resume", "abc"},
			append(append(base[:len(base):len(base)], atoll...), "--resume", "abc"), atollSecrets},
		{"system on resume", provider.Config{SystemPrompt: "addition"}, []string{"--resume", "abc"},
			append(base[:len(base):len(base)], "--append-system-prompt", "addition", "--resume", "abc"), nil},
		{"tokens, headers and env stay out of argv", provider.Config{
			MCPURL:       "http://127.0.0.1:9/mcp/t0k",
			GitHubMCPURL: "https://api.githubcopilot.com/mcp/",
			GitHubToken:  "gho_abc",
			MCPServers: []provider.UserMCPServer{
				{Name: "docs", URL: "https://docs.example/mcp?key=k1", Headers: map[string]string{"X-Key": "h1", "Authorization": "Bearer b1"}},
				{Name: "local", Command: "srv", Args: []string{"--stdio"}, Env: map[string]string{"TOKEN": "e1"}},
			},
		}, nil,
			append(base[:len(base):len(base)], mcp(`{"mcpServers":{`+
				`"atoll":{"type":"http","url":"${ATOLL_MCP_0}"},`+
				`"docs":{"headers":{"Authorization":"${ATOLL_MCP_3}","X-Key":"${ATOLL_MCP_4}"},"type":"http","url":"${ATOLL_MCP_2}"},`+
				`"github":{"headers":{"Authorization":"Bearer ${ATOLL_MCP_1}"},"type":"http","url":"https://api.githubcopilot.com/mcp/"},`+
				`"local":{"args":["--stdio"],"command":"srv","env":{"TOKEN":"${ATOLL_MCP_5}"},"type":"stdio"}`+
				`}}`)...),
			[]string{
				"ATOLL_MCP_0=http://127.0.0.1:9/mcp/t0k", "ATOLL_MCP_1=gho_abc",
				"ATOLL_MCP_2=https://docs.example/mcp?key=k1", "ATOLL_MCP_3=Bearer b1", "ATOLL_MCP_4=h1",
				"ATOLL_MCP_5=e1",
			}},
		{"title without tools", provider.Config{NoTools: true}, nil,
			noTools, nil},
		{"no tools ignores MCP", provider.Config{MCPURL: "http://127.0.0.1:9/mcp/t0k", NoTools: true}, nil,
			noTools, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, secrets, err := argv(tt.cfg, tt.extra...)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argv = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(secrets, tt.wantSecrets) {
				t.Errorf("secrets = %q, want %q", secrets, tt.wantSecrets)
			}
		})
	}
}

func TestStartKeepsSecretsOutOfArgv(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + dir + "/args\nenv > " + dir + "/env\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	s := New()
	cfg := provider.Config{
		Bin: bin, Dir: t.TempDir(), Cwd: t.TempDir(),
		MCPURL: "http://127.0.0.1:9/mcp/t0k", GitHubMCPURL: "https://api.githubcopilot.com/mcp/", GitHubToken: "gho_abc",
		MCPServers: []provider.UserMCPServer{{Name: "local", Command: "srv", Env: map[string]string{"TOKEN": "e1"}}},
	}
	if err := s.Start(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Interrupt() })
	for range s.Events() {
	}

	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(filepath.Join(dir, "env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"t0k", "gho_abc", "e1"} {
		if strings.Contains(string(args), secret) {
			t.Errorf("argv holds %q:\n%s", secret, args)
		}
	}
	for _, want := range []string{
		"ATOLL_MCP_0=http://127.0.0.1:9/mcp/t0k\n", "ATOLL_MCP_1=gho_abc\n", "ATOLL_MCP_2=e1\n",
	} {
		if !strings.Contains(string(env), want) {
			t.Errorf("environment lacks %q", want)
		}
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
	const (
		worktree = "-data-trees-seeded-fix-seeded-bug"
		checkout = "-data-threads-t1"
	)
	tests := []struct {
		name string
		// files maps a project folder to its content and age (seconds before now).
		files map[string]struct {
			body string
			age  int
		}
		cwd, want string
	}{
		{"copies from another folder", map[string]struct {
			body string
			age  int
		}{checkout: {"history", 0}}, "/data/trees/seeded/fix-seeded.bug", "history"},
		{"newer elsewhere replaces the stale copy", map[string]struct {
			body string
			age  int
		}{worktree: {"stale", 60}, checkout: {"latest", 0}}, "/data/trees/seeded/fix-seeded.bug", "latest"},
		{"newer here is kept", map[string]struct {
			body string
			age  int
		}{worktree: {"latest", 0}, checkout: {"stale", 60}}, "/data/trees/seeded/fix-seeded.bug", "latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for folder, f := range tt.files {
				path := filepath.Join(dir, "projects", folder, "s1.jsonl")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(f.body), 0o600); err != nil {
					t.Fatal(err)
				}
				at := time.Now().Add(-time.Duration(f.age) * time.Second)
				if err := os.Chtimes(path, at, at); err != nil {
					t.Fatal(err)
				}
			}

			if err := adoptSession(provider.Config{Dir: dir, Cwd: tt.cwd}, "s1"); err != nil {
				t.Fatal(err)
			}

			to := filepath.Join(dir, "projects", worktree, "s1.jsonl")
			if got, err := os.ReadFile(to); err != nil || string(got) != tt.want {
				t.Errorf("session = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
