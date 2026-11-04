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
			name:        "two agent messages are split",
			prompt:      _skills,
			wantReply:   "I’ll check the connected MCP resource and template registries so the server list is complete. No skills catalog was provided in this session.\n\n- Skills: none.\n- MCP servers: none connected or exposed.",
			wantSession: _threadID,
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
			cfg := provider.Config{Bin: bin, Dir: t.TempDir(), Cwd: t.TempDir()}
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
			if err := s.Send(ctx, tt.prompt); err != nil {
				t.Fatal(err)
			}
			var reply strings.Builder
			session := ""
			for ev := range s.Events() {
				switch ev.Kind {
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
			if reply.String() != tt.wantReply || session != tt.wantSession {
				t.Errorf("got reply %q session %q, want reply %q session %q",
					reply.String(), session, tt.wantReply, tt.wantSession)
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
