// Command claude is a fake Claude Code for the e2e tests. It answers --version and
// `auth status`, and in -p stream-json mode replays the recording in e2e/testdata/claude whose
// prompt the user message on stdin contains. Like the real CLI it calls the HTTP MCP servers of
// --mcp-config when a recording uses their tools, and asks --permission-prompt-tool before a call
// the recording marks with a "fake" ask line, unless --allowedTools allows it. Signed in means
// CLAUDE_CONFIG_DIR (default ~/.claude) holds .credentials.json; a revoked one (fake.Refused)
// fails the next turn as the real CLI does. Every call is logged to <executable>.log.
package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"atoll/e2e/fakes/fake"
)

const (
	_version  = "2.1.306 (Claude Code)"
	_loginURL = "https://login.reef.test/claude/oauth?code=true"
)

// _models is what the initialize control request answers, in the real CLI's shape (verified on
// Claude Code 2.1.317, signed out and offline). reef-probe proves a list came from the provider.
var _models = []map[string]string{
	{"value": "default", "displayName": "Default (recommended)", "description": "Use the default model"},
	{"value": "opus", "displayName": "Opus", "description": "Opus 5.5"},
	{"value": "sonnet", "displayName": "Sonnet", "description": "Sonnet 5.5"},
	{"value": "reef-probe", "displayName": "Reef Probe", "description": "Only the fake lists this"},
}

type (
	line struct {
		Type      string  `json:"type"`
		Subtype   string  `json:"subtype"`
		SessionID string  `json:"session_id"`
		IsReplay  bool    `json:"isReplay"`
		Message   message `json:"message"`
		RequestID string  `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}

	message struct {
		Content json.RawMessage `json:"content"`
	}

	recording struct {
		session string
		prompt  string
		resumed bool // only plays on --resume of its session
		lines   []string
	}

	authStatus struct {
		LoggedIn        bool   `json:"loggedIn"`
		AuthMethod      string `json:"authMethod"`
		ConfigDirectory string `json:"configDirectory"`
	}
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fake claude:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	log, err := fake.Start()
	if err != nil {
		return err
	}
	switch {
	case slices.Contains(args, "--version"):
		fmt.Println(_version)
		return nil
	case len(args) >= 2 && args[0] == "auth" && args[1] == "status":
		return status()
	case len(args) >= 2 && args[0] == "auth" && args[1] == "login":
		return fake.LoginWithCode(fake.ConfigDir("CLAUDE_CONFIG_DIR", ".claude"), ".credentials.json",
			"Opening browser to sign in…",
			"If the browser didn't open, visit: "+_loginURL)
	case slices.Contains(args, "-p") || slices.Contains(args, "--print"):
		return stream(log, args)
	}
	return fmt.Errorf("unsupported arguments %q", args)
}

func status() error {
	dir := fake.ConfigDir("CLAUDE_CONFIG_DIR", ".claude")
	_, err := os.Stat(filepath.Join(dir, ".credentials.json"))
	st := authStatus{LoggedIn: err == nil, AuthMethod: "none", ConfigDirectory: dir}
	if st.LoggedIn {
		st.AuthMethod = "claude.ai"
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	if !st.LoggedIn {
		os.Exit(1)
	}
	return nil
}

func stream(log *fake.Log, args []string) error {
	if flag(args, "--input-format") != "stream-json" || flag(args, "--output-format") != "stream-json" {
		return errors.New("want --input-format stream-json --output-format stream-json")
	}
	if slices.Contains(args, "--system-prompt") || slices.Contains(args, "--system-prompt-file") {
		return errors.New("reef must append, not replace the provider prompt")
	}
	recs, err := load()
	if err != nil {
		return err
	}
	p, err := newPlayer(args)
	if err != nil {
		return err
	}
	if err := autoMemory(args); err != nil {
		return err
	}
	resume := flag(args, "--resume")
	titling := strings.HasPrefix(flag(args, "--append-system-prompt"), fake.TitleSystem)
	in := fake.Lines()
	for in.Scan() {
		if err := log.Stdin(in.Text()); err != nil {
			return err
		}
		var l line
		if json.Unmarshal(in.Bytes(), &l) != nil {
			continue
		}
		if l.Type == "control_request" && l.Request.Subtype == "initialize" {
			b, _ := json.Marshal(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": l.RequestID, "response": map[string]any{"models": _models},
			}})
			fmt.Println(string(b))
			continue
		}
		if l.Type != "user" {
			continue
		}
		if titling {
			prompt, err := text(l.Message.Content)
			if err != nil {
				return err
			}
			if err := title(prompt); err != nil {
				return err
			}
			continue
		}
		// Logged at the first turn, so a model-list probe leaves no prompt or skill record.
		if !p.started && p.userMessage == "" {
			if err := log.Prompt("system", flag(args, "--append-system-prompt")); err != nil {
				return err
			}
			if err := fake.LogSkills(p.skills); err != nil {
				return err
			}
		}
		prompt, err := text(l.Message.Content)
		if err != nil {
			return err
		}
		if err := log.Prompt("user", prompt); err != nil {
			return err
		}
		p.userMessage = prompt
		if fake.Refused(fake.ConfigDir("CLAUDE_CONFIG_DIR", ".claude"), ".credentials.json") {
			rec, _ := find(recs, prompt, resume)
			signedOut(cmp.Or(rec.session, resume, "0b5e7a1c-3f2d-4c8e-9a61-7d4f2e8b1c90"))
		}
		rec, ok := find(recs, prompt, resume)
		if !ok {
			return fmt.Errorf("no recording for prompt %q (resume %q)", prompt, resume)
		}
		if err := p.play(rec.lines); err != nil {
			return err
		}
	}
	return in.Err()
}

// signedOut ends the turn the way claude does when the server refuses its OAuth token: the init
// line, a synthetic assistant message marked authentication_failed, an error result, exit 1.
func signedOut(session string) {
	const msg = "Failed to authenticate: OAuth token revoked"
	cwd, _ := os.Getwd()
	for _, l := range []map[string]any{
		{"type": "system", "subtype": "init", "cwd": cwd, "session_id": session, "tools": []string{}, "mcp_servers": []any{},
			"model": "claude-opus-5-5", "permissionMode": "default", "apiKeySource": "none", "claude_code_version": "2.1.306"},
		{"type": "assistant", "message": map[string]any{"id": "c1a7e2b0-5d3f-4e9a-8b21-6f0d9c4e3a17", "model": "<synthetic>",
			"role": "assistant", "type": "message", "stop_reason": "stop_sequence", "stop_sequence": "",
			"content": []map[string]string{{"type": "text", "text": msg}}},
			"parent_tool_use_id": nil, "session_id": session, "error": "authentication_failed"},
		{"type": "result", "subtype": "success", "is_error": true, "duration_ms": 212, "duration_api_ms": 0,
			"num_turns": 1, "result": msg, "stop_reason": "stop_sequence", "session_id": session, "total_cost_usd": 0,
			"permission_denials": []any{}},
	} {
		b, _ := json.Marshal(l)
		fmt.Println(string(b))
	}
	os.Exit(1)
}

// title answers a title session's turn with fake.TitleFor in one result line.
func title(prompt string) error {
	t, ok := fake.TitleFor(prompt)
	if !ok {
		return fmt.Errorf("no title for prompt %q", prompt)
	}
	const session = "3d9f1c7a-2b4e-4f6a-9c1d-8e7b5a3f2c10"
	for _, l := range []map[string]any{
		{"type": "system", "subtype": "init", "session_id": session},
		{"type": "result", "subtype": "success", "is_error": false, "result": t, "session_id": session},
	} {
		b, _ := json.Marshal(l)
		fmt.Println(string(b))
	}
	return nil
}

// text is the text of a user message: a string, or the text blocks of a content array.
func text(content json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s, nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return "", fmt.Errorf("user message content is neither a string nor blocks: %s", content)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// find returns the recording whose prompt the message contains, the longest if several do.
// With a resume id it must belong to that session; without one it must not be marked resumed.
func find(recs []recording, prompt, resume string) (recording, bool) {
	var best recording
	for _, r := range recs {
		if r.prompt != "" && strings.Contains(prompt, r.prompt) && (resume == "" && !r.resumed || r.session == resume) &&
			len(r.prompt) > len(best.prompt) {
			best = r
		}
	}
	return best, best.prompt != ""
}

func load() ([]recording, error) {
	all, err := fake.Recordings("claude")
	if err != nil {
		return nil, err
	}
	recs := make([]recording, 0, len(all))
	for _, lines := range all {
		r := recording{lines: lines}
		for _, s := range lines {
			if fake.Directive([]byte(s)) == "resumed" {
				r.resumed = true
				continue
			}
			var l line
			if err := json.Unmarshal([]byte(s), &l); err != nil {
				return nil, err
			}
			if l.Type == "system" && l.Subtype == "init" {
				r.session = l.SessionID
			}
			if l.Type == "user" && l.IsReplay {
				_ = json.Unmarshal(l.Message.Content, &r.prompt)
			}
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// autoMemory makes the memory folder real claude keeps per project under its config folder,
// unless --settings or the environment turns auto memory off.
func autoMemory(args []string) error {
	var s struct {
		AutoMemoryEnabled *bool `json:"autoMemoryEnabled"`
	}
	_ = json.Unmarshal([]byte(flag(args, "--settings")), &s)
	if os.Getenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY") == "1" || s.AutoMemoryEnabled != nil && !*s.AutoMemoryEnabled {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	project := strings.NewReplacer("/", "-", ".", "-").Replace(cwd)
	return os.MkdirAll(filepath.Join(fake.ConfigDir("CLAUDE_CONFIG_DIR", ".claude"), "projects", project, "memory"), 0o755)
}

// flag returns the value after name in args, or "".
func flag(args []string, name string) string {
	i := slices.Index(args, name)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}
