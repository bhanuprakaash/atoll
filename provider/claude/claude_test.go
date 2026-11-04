package claude

import (
	"context"
	"os/exec"
	"path/filepath"
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
