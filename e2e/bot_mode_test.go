package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Contract: bot-mode-contract.md.

var _modes = []string{"auto", "ask", "full"}

func TestBotMode_APIRoundTrip(t *testing.T) {
	_, s := start(t, "claude")
	var plain botInfo
	s.do(t, "POST", "/api/bots", map[string]string{"name": "Plain", "persona": "x", "provider": "claude"}, &plain, http.StatusCreated)
	if got := s.bot(t, plain.ID).Mode; plain.Mode != "auto" || got != "auto" {
		t.Errorf("Bot created without a mode: POST mode %q, GET mode %q, want auto", plain.Mode, got)
	}

	for _, mode := range _modes {
		var created, edited botInfo
		s.do(t, "POST", "/api/bots", map[string]string{"name": "Made " + mode, "persona": "x", "provider": "claude", "mode": mode}, &created, http.StatusCreated)
		if got := s.bot(t, created.ID).Mode; created.Mode != mode || got != mode {
			t.Errorf("POST mode %s: reply mode %q, GET mode %q", mode, created.Mode, got)
		}
		s.do(t, "PUT", "/api/bots/"+plain.ID, map[string]string{"mode": mode}, &edited, http.StatusOK)
		if got := s.bot(t, plain.ID).Mode; edited.Mode != mode || got != mode {
			t.Errorf("PUT mode %s: reply mode %q, GET mode %q", mode, edited.Mode, got)
		}
	}
	var bots []botInfo
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	for _, b := range bots {
		if b.ID == plain.ID && b.Mode != "full" {
			t.Errorf("GET /api/bots lists Plain with mode %q, want full", b.Mode)
		}
	}

	rejected(t, s, "PUT", "/api/bots/"+plain.ID, map[string]string{"mode": "yolo"}, _modes)
	if got := s.bot(t, plain.ID).Mode; got != "full" {
		t.Errorf("refused PUT mode yolo changed mode to %q", got)
	}
	rejected(t, s, "POST", "/api/bots", map[string]string{"name": "Yolo", "persona": "x", "provider": "claude", "mode": "yolo"}, _modes)
	var after []botInfo
	s.do(t, "GET", "/api/bots", nil, &after, http.StatusOK)
	if len(after) != len(bots) {
		t.Errorf("refused POST mode yolo created a Bot: %d Bots, want %d", len(after), len(bots))
	}
}

func TestBotMode_LaunchMovesOldBotsToAutoOnce(t *testing.T) {
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	m.signIn(t, "codex")
	old, err := os.ReadFile(filepath.Join("testdata", "before-modes.sql"))
	if err != nil {
		t.Fatal(err)
	}
	dbExec(t, m, string(old))
	s := m.serve(t)

	var bots []botInfo
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	if len(bots) != 3 {
		t.Fatalf("GET /api/bots after the old install's launch: %+v, want Chief and two Bots", bots)
	}
	for _, b := range bots {
		if b.Mode != "auto" {
			t.Errorf("Bot %s of an old install has mode %q after a launch, want auto", b.Name, b.Mode)
		}
	}

	const oldClaude, oldCodex = "c31d276614b94b87", "34bf601fa249914c"
	s.do(t, "PUT", "/api/bots/"+oldClaude, map[string]string{"mode": "ask"}, nil, http.StatusOK)
	s.kill()
	s = m.serve(t)
	if got := s.bot(t, oldClaude).Mode; got != "ask" {
		t.Errorf("Bot set to ask after the move: mode %q after a restart, want ask", got)
	}
	if got := s.bot(t, oldCodex).Mode; got != "auto" {
		t.Errorf("untouched old Bot: mode %q after a restart, want auto", got)
	}
}

func TestBotMode_ReachesProvider(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			for _, mode := range _modes {
				var b bot
				s.do(t, "POST", "/api/bots", map[string]string{"name": "Bot " + mode, "persona": "x", "provider": p, "mode": mode}, &b, http.StatusCreated)
				th := s.newThread(t, b)
				if r := s.finished(t, s.events(t, th), s.send(t, th, _keepPrompt).ID); r.Status != "done" {
					t.Fatalf("mode %s run: %+v", mode, r)
				}
				sessions := m.sessions(t, p)
				checkMode(t, p, mode, sessions[len(sessions)-1])
			}
		})
	}
}

func TestBotMode_Chief(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			id := createdID(t, chiefCall(t, s, "create_bot", map[string]any{"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "mode": "ask"}))
			if got := s.bot(t, id).Mode; got != "ask" {
				t.Fatalf("create_bot mode ask: GET mode %q", got)
			}
			plain := createdID(t, chiefCall(t, s, "create_bot", map[string]any{"name": "Plain", "persona": "x", "provider": p}))
			if got := s.bot(t, plain).Mode; got != "auto" {
				t.Errorf("create_bot without mode: GET mode %q, want auto", got)
			}

			edit := chiefCall(t, s, "edit_bot", map[string]any{"id": id, "mode": "full"})
			change := platformChange(t, edit.events, "chief", edit.thread, edit.run, "bot", id, "edit")
			if got := s.bot(t, id).Mode; got != "full" {
				t.Fatalf("edit_bot mode full: GET mode %q", got)
			}

			var listed []map[string]any
			if list := chiefCall(t, s, "list_bots", map[string]any{}); json.Unmarshal([]byte(list.reply), &listed) != nil {
				t.Fatalf("list_bots result %q, want the Bots as JSON", list.reply)
			}
			modes := map[any]any{}
			for _, b := range listed {
				modes[b["id"]] = b["mode"]
			}
			if modes[id] != "full" || modes[plain] != "auto" || modes["chief"] != "auto" {
				t.Errorf("list_bots modes by id %v, want %s full, %s auto, chief auto", modes, id, plain)
			}

			failsListing(t, chiefCall(t, s, "edit_bot", map[string]any{"id": id, "mode": "yolo"}), _modes)
			if got := s.bot(t, id).Mode; got != "full" {
				t.Fatalf("edit_bot mode yolo changed mode to %q", got)
			}

			s.do(t, "POST", "/api/platform/changes/"+change+"/undo", nil, nil, http.StatusNoContent)
			if got := s.bot(t, id).Mode; got != "ask" {
				t.Errorf("undo edit_bot mode full: mode %q, want ask", got)
			}
		})
	}
}

func TestBotMode_ChangeMidRunAppliesFromNextRun(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			ev := s.events(t, th)
			first := s.send(t, th, _writePrompt)
			approval := nextOf(t, ev, first.ID, "approval")

			s.do(t, "PUT", "/api/bots/"+b.ID, map[string]string{"mode": "full"}, nil, http.StatusOK)
			s.do(t, "POST", "/api/approvals/"+approval, map[string]bool{"approve": true}, nil, http.StatusNoContent)
			if r := s.finished(t, ev, first.ID); r.Status != "done" {
				t.Fatalf("run going while the mode changed: %+v", r)
			}
			if r := s.finished(t, ev, s.send(t, th, _keepPrompt).ID); r.Status != "done" {
				t.Fatalf("run after the mode changed: %+v", r)
			}

			sessions := m.sessions(t, p)
			if len(sessions) != 2 {
				t.Fatalf("%s ran %d sessions, want 2", p, len(sessions))
			}
			checkMode(t, p, "auto", sessions[0])
			checkMode(t, p, "full", sessions[1])
		})
	}
}

// checkMode checks that a session of provider p ran with mode's flags (Claude) or thread params
// (Codex).
func checkMode(t *testing.T, p, mode string, c *call) {
	t.Helper()
	if p == "claude" {
		pm, prompt := flagValue(c.argv, "--permission-mode"), flagValue(c.argv, "--permission-prompt-tool")
		switch mode {
		case "auto":
			if pm != "auto" || prompt != "mcp__atoll__approve" {
				t.Errorf("claude on auto: --permission-mode %q, --permission-prompt-tool %q; want auto and mcp__atoll__approve", pm, prompt)
			}
		case "ask":
			if (pm != "" && pm != "default") || prompt != "mcp__atoll__approve" {
				t.Errorf("claude on ask: --permission-mode %q, --permission-prompt-tool %q; want the default mode and mcp__atoll__approve", pm, prompt)
			}
		case "full":
			if pm != "bypassPermissions" {
				t.Errorf("claude on full: --permission-mode %q, want bypassPermissions", pm)
			}
		}
		return
	}
	sandbox, policy := codexPolicy(c)
	switch mode {
	case "auto":
		if sandbox != "workspace-write" || policy != "on-request" {
			t.Errorf("codex on auto: sandbox %q, approvalPolicy %q; want workspace-write, on-request", sandbox, policy)
		}
	case "ask":
		if sandbox == "danger-full-access" || policy != "untrusted" {
			t.Errorf("codex on ask: sandbox %q, approvalPolicy %q; want a sandbox other than danger-full-access, untrusted", sandbox, policy)
		}
	case "full":
		if sandbox != "danger-full-access" || policy != "never" {
			t.Errorf("codex on full: sandbox %q, approvalPolicy %q; want danger-full-access, never", sandbox, policy)
		}
	}
}

// codexPolicy is the sandbox and approval policy a codex session's thread/start or thread/resume
// asked for, as params or in its config.
func codexPolicy(c *call) (sandbox, policy string) {
	for _, line := range c.stdin {
		var r struct {
			Method string `json:"method"`
			Params struct {
				Sandbox        string `json:"sandbox"`
				ApprovalPolicy string `json:"approvalPolicy"`
				Config         struct {
					SandboxMode    string `json:"sandbox_mode"`
					ApprovalPolicy string `json:"approval_policy"`
				} `json:"config"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &r) == nil && (r.Method == "thread/start" || r.Method == "thread/resume") {
			return r.Params.Sandbox + r.Params.Config.SandboxMode, r.Params.ApprovalPolicy + r.Params.Config.ApprovalPolicy
		}
	}
	return "", ""
}
