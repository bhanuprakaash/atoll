// Command codex is a fake Codex for the e2e tests. It answers --version and `login status`,
// and as `codex app-server` speaks JSON-RPC on stdin and stdout, replaying the recordings in
// e2e/testdata/codex: a turn/start gets the recording whose prompt the input contains; one
// recorded on a resumed thread only plays on a resumed thread. Like the real app-server it calls
// the HTTP MCP servers of its config (mcp_servers.<name>.url) when a recording uses their tools,
// and sends a recorded approval request unless approvalPolicy is "never". A turn/steer whose
// expectedTurnId is not the turn going is refused, as app-server refuses it. Methods no recording
// has get an empty result. Signed in means CODEX_HOME (default ~/.codex) holds auth.json; a
// revoked one (fake.Refused) fails the next turn as the real app-server does. Every call is logged
// to <executable>.log.
package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"atoll/e2e/fakes/fake"
)

const (
	_version  = "codex-cli 0.166.0"
	_loginURL = "https://login.reef.test/codex/oauth?client_id=fake"
)

// _models answers model/list in the real app-server's shape (verified on codex-cli 0.165,
// signed out and offline). reef-probe-codex proves a list came from the provider.
const _models = `{"data":[{"id":"gpt-6.1-sol","model":"gpt-6.1-sol","displayName":"GPT-6.1-Sol","description":"Workhorse","hidden":false,"isDefault":true},` +
	`{"id":"reef-probe-codex","model":"reef-probe-codex","displayName":"Reef Probe Codex","description":"Only the fake lists this","hidden":false,"isDefault":false}],"nextCursor":null}`

type (
	entry struct {
		Fake string          `json:"fake"`
		MS   int             `json:"ms"`
		Dir  string          `json:"dir"`
		Msg  json.RawMessage `json:"msg"`
	}

	message struct {
		ID     json.RawMessage `json:"id,omitempty"`
		Method string          `json:"method,omitempty"`
		Params json.RawMessage `json:"params,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  json.RawMessage `json:"error,omitempty"`
	}

	// recording is one recorded app-server conversation.
	recording struct {
		thread  string
		prompt  string
		replies map[string]json.RawMessage // result of each request, by method
		turn    []entry                    // what follows the turn/start reply, through turn/completed
	}

	threadParams struct {
		DeveloperInstructions *string        `json:"developerInstructions"`
		BaseInstructions      *string        `json:"baseInstructions"`
		Cwd                   string         `json:"cwd"`
		ThreadID              string         `json:"threadId"`
		ApprovalPolicy        string         `json:"approvalPolicy"`
		Config                map[string]any `json:"config"`
		Input                 []struct {
			Text string `json:"text"`
		} `json:"input"`
	}
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fake codex:", err)
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
	case slices.Contains(args, "login") && slices.Contains(args, "status"):
		return status()
	case len(args) == 1 && args[0] == "login":
		return fake.Login(fake.ConfigDir("CODEX_HOME", ".codex"), "auth.json",
			"Starting local login server on http://localhost:1455.",
			"If your browser did not open, navigate to this URL to authenticate:",
			"",
			_loginURL)
	case slices.Contains(args, "app-server"):
		return serve(log, args)
	}
	return fmt.Errorf("unsupported arguments %q", args)
}

func status() error {
	dir := fake.ConfigDir("CODEX_HOME", ".codex")
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); err != nil {
		fmt.Fprintln(os.Stderr, "Not logged in")
		os.Exit(1)
	}
	fmt.Println("Logged in using ChatGPT")
	return nil
}

func serve(log *fake.Log, args []string) error {
	recs, err := load()
	if err != nil {
		return err
	}
	cfg := map[string]string{}
	for i, a := range args {
		if (a == "-c" || a == "--config") && i+1 < len(args) {
			k, v, _ := strings.Cut(args[i+1], "=")
			cfg[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	var extraRoots []string
	var thread *recording
	var t *turn
	titling := false
	in := fake.Lines()
	for in.Scan() {
		if err := log.Stdin(in.Text()); err != nil {
			return err
		}
		var m message
		if json.Unmarshal(in.Bytes(), &m) != nil || m.Method == "" || len(m.ID) == 0 {
			continue
		}
		var p threadParams
		_ = json.Unmarshal(m.Params, &p)
		if m.Method == "thread/start" || m.Method == "thread/resume" {
			if p.BaseInstructions != nil || p.Config["base_instructions"] != nil || cfg["base_instructions"] != "" {
				return fmt.Errorf("reef must preserve baseInstructions")
			}
			addition := cfg["developer_instructions"]
			if v, ok := p.Config["developer_instructions"].(string); ok {
				addition = v
			}
			if p.DeveloperInstructions != nil {
				addition = *p.DeveloperInstructions
			}
			titling = strings.HasPrefix(addition, fake.TitleSystem)
			if !titling {
				if err := log.Prompt("system", addition); err != nil {
					return err
				}
			}
		}
		switch m.Method {
		case "model/list":
			reply(m.ID, json.RawMessage(_models), "")
		case "skills/extraRoots/set":
			var roots struct {
				ExtraRoots []string `json:"extraRoots"`
			}
			if err := json.Unmarshal(m.Params, &roots); err != nil {
				return err
			}
			extraRoots = roots.ExtraRoots
			reply(m.ID, json.RawMessage(`{}`), "")
		case "thread/start":
			thread = find(recs, func(r recording) bool { return r.replies[m.Method] != nil })
			t = newTurn(cfg, p, false, extraRoots)
			reply(m.ID, thread.replies[m.Method], "")
		case "thread/resume":
			thread = find(recs, func(r recording) bool { return r.thread == p.ThreadID && r.replies[m.Method] != nil })
			if thread == nil {
				reply(m.ID, nil, fmt.Sprintf("no rollout found for thread id %s", p.ThreadID))
				continue
			}
			t = newTurn(cfg, p, true, extraRoots)
			reply(m.ID, thread.replies[m.Method], "")
		case "turn/start":
			prompt := ""
			if len(p.Input) > 0 {
				prompt = p.Input[0].Text
			}
			if titling {
				title(m.ID, p.ThreadID, prompt)
				continue
			}
			if err := log.Prompt("user", prompt); err != nil {
				return err
			}
			if t != nil {
				t.userMessage = prompt
			}
			if fake.Refused(fake.ConfigDir("CODEX_HOME", ".codex"), "auth.json") {
				signedOut(m.ID, p.ThreadID)
				continue
			}
			if at, ok := fake.Limited(fake.ConfigDir("CODEX_HOME", ".codex"), "auth.json"); ok {
				limited(m.ID, p.ThreadID, at)
				continue
			}
			var rec *recording
			for i, r := range recs {
				if r.prompt != "" && strings.Contains(prompt, r.prompt) && thread != nil && r.thread == thread.thread &&
					(r.replies["thread/resume"] == nil || t.resumed) && (rec == nil || len(r.prompt) > len(rec.prompt)) {
					rec = &recs[i]
				}
			}
			if rec == nil {
				reply(m.ID, nil, fmt.Sprintf("fake codex: no recording for prompt %q", prompt))
				continue
			}
			var started struct {
				Turn struct {
					ID string `json:"id"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(rec.replies[m.Method], &started)
			t.id = started.Turn.ID
			reply(m.ID, rec.replies[m.Method], "")
			if err := t.play(rec.turn, in, log); err != nil {
				return err
			}
		default:
			result := json.RawMessage(`{}`)
			if r := find(recs, func(r recording) bool { return r.replies[m.Method] != nil }); r != nil {
				result = r.replies[m.Method]
			}
			reply(m.ID, result, "")
		}
	}
	return in.Err()
}

// signedOut fails the turn the way the app-server does when the ChatGPT login can no longer be
// refreshed: the turn starts, then an error notification and a failed turn/completed, both
// marked unauthorized. The app-server keeps running.
func signedOut(id json.RawMessage, thread string) {
	const turn = "01a1a5d2-7c4e-7b19-9f30-2e8d6a1c5b47"
	e := map[string]any{
		"message":           "Your access token could not be refreshed because your refresh token was revoked. Please log out and sign in again.",
		"codexErrorInfo":    "unauthorized",
		"additionalDetails": nil,
	}
	started := map[string]any{"id": turn, "items": []any{}, "status": "inProgress", "error": nil}
	b, _ := json.Marshal(map[string]any{"turn": started})
	reply(id, b, "")
	for _, n := range []map[string]any{
		{"method": "turn/started", "params": map[string]any{"threadId": thread, "turn": started}},
		{"method": "error", "params": map[string]any{"error": e, "willRetry": false, "threadId": thread, "turnId": turn}},
		{"method": "turn/completed", "params": map[string]any{"threadId": thread,
			"turn": map[string]any{"id": turn, "items": []any{}, "status": "failed", "error": e}}},
	} {
		b, _ := json.Marshal(n)
		fmt.Println(string(b))
	}
}

// limited fails the turn the way the app-server does when the ChatGPT plan's usage limit is
// reached: the turn starts, an account/rateLimits/updated snapshot says which window is full and
// when it resets, then an error notification and a failed turn/completed, both marked
// usageLimitExceeded. The message holds no time, as the app-server's does when the backend sent
// none; the weekly window, further from full, resets later. The app-server keeps running.
func limited(id json.RawMessage, thread string, at time.Time) {
	const turn = "01a1a5d2-7c4e-7b19-9f30-8b3c4d6e2f51"
	e := map[string]any{
		"message":           "You've hit your usage limit. Upgrade to Plus to continue using Codex (https://chatgpt.com/explore/plus), or try again later.",
		"codexErrorInfo":    "usageLimitExceeded",
		"additionalDetails": nil,
	}
	limits := map[string]any{
		"limitId": "codex", "limitName": nil, "planType": "plus", "rateLimitReachedType": "rate_limit_reached",
		"primary":   map[string]any{"usedPercent": 100, "windowDurationMins": 300, "resetsAt": at.Unix()},
		"secondary": map[string]any{"usedPercent": 41, "windowDurationMins": 10080, "resetsAt": at.Add(6 * 24 * time.Hour).Unix()},
		"credits":   nil,
	}
	started := map[string]any{"id": turn, "items": []any{}, "status": "inProgress", "error": nil}
	b, _ := json.Marshal(map[string]any{"turn": started})
	reply(id, b, "")
	for _, n := range []map[string]any{
		{"method": "turn/started", "params": map[string]any{"threadId": thread, "turn": started}},
		{"method": "account/rateLimits/updated", "params": map[string]any{"rateLimits": limits}},
		{"method": "error", "params": map[string]any{"error": e, "willRetry": false, "threadId": thread, "turnId": turn}},
		{"method": "turn/completed", "params": map[string]any{"threadId": thread,
			"turn": map[string]any{"id": turn, "items": []any{}, "status": "failed", "error": e}}},
	} {
		b, _ := json.Marshal(n)
		fmt.Println(string(b))
	}
}

// title answers a title session's turn with fake.TitleFor in one message delta, or fails it.
func title(id json.RawMessage, thread, prompt string) {
	t, ok := fake.TitleFor(prompt)
	if !ok {
		reply(id, nil, fmt.Sprintf("fake codex: no title for prompt %q", prompt))
		return
	}
	const turn = "01a1a5d2-7c4e-7b19-9f30-5e1b7a2c9d40"
	b, _ := json.Marshal(map[string]any{"turn": map[string]any{"id": turn, "items": []any{}, "status": "inProgress"}})
	reply(id, b, "")
	for _, n := range []map[string]any{
		{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": thread, "turnId": turn, "delta": t}},
		{"method": "turn/completed", "params": map[string]any{"threadId": thread,
			"turn": map[string]any{"id": turn, "items": []any{}, "status": "completed"}}},
	} {
		b, _ := json.Marshal(n)
		fmt.Println(string(b))
	}
}

func reply(id, result json.RawMessage, errMsg string) {
	m := message{ID: id, Result: result}
	if errMsg != "" {
		m.Error, _ = json.Marshal(map[string]any{"code": -32600, "message": errMsg})
	}
	b, _ := json.Marshal(m)
	fmt.Println(string(b))
}

func find(recs []recording, ok func(recording) bool) *recording {
	for i := range recs {
		if ok(recs[i]) {
			return &recs[i]
		}
	}
	return nil
}

// load reads every recording, in file name order.
func load() ([]recording, error) {
	all, err := fake.Recordings("codex")
	if err != nil {
		return nil, err
	}
	recs := make([]recording, 0, len(all))
	for _, name := range slices.Sorted(maps.Keys(all)) {
		r, err := parse(all[name])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		recs = append(recs, r)
	}
	return recs, nil
}

func parse(lines []string) (recording, error) {
	r := recording{replies: map[string]json.RawMessage{}}
	methods := map[string]string{} // request id -> method
	inTurn := false
	for _, s := range lines {
		var e entry
		if err := json.Unmarshal([]byte(s), &e); err != nil {
			return r, err
		}
		var m message
		if e.Msg != nil {
			if err := json.Unmarshal(e.Msg, &m); err != nil {
				return r, err
			}
		}
		switch {
		case e.Dir == "in" && m.Method != "" && len(m.ID) > 0:
			methods[string(m.ID)] = m.Method
			var p threadParams
			_ = json.Unmarshal(m.Params, &p)
			if m.Method == "turn/start" && len(p.Input) > 0 {
				r.prompt, r.thread = p.Input[0].Text, p.ThreadID
			}
		case e.Dir == "out" && m.Method == "" && len(m.ID) > 0:
			method := methods[string(m.ID)]
			r.replies[method] = m.Result
			inTurn = method == "turn/start"
		case inTurn && (e.Dir == "out" || e.Fake != ""):
			r.turn = append(r.turn, e)
		}
	}
	return r, nil
}
