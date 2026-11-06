package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"atoll/e2e/fakes/fake"

	"github.com/BurntSushi/toml"
)

type (
	// turn plays recorded turns the way app-server would have in this thread: MCP tool calls
	// really go to the configured servers, approval requests to the client.
	turn struct {
		skills      []fake.SkillCopy
		userMessage string
		cwd         string
		servers     map[string]*fake.MCP
		never       bool // approvalPolicy "never": nothing is asked, so everything asked is declined
		resumed     bool
	}

	item struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}

	itemParams struct {
		Item item `json:"item"`
	}
)

// newTurn connects the MCP servers of the -c config and the thread's config.
func newTurn(cfg map[string]string, p threadParams, resumed bool, extra []string) *turn {
	all := map[string]string{}
	for k, v := range cfg {
		all[k] = v
	}
	flat := map[string]string{}
	flatten("", p.Config, flat)
	for k, v := range flat {
		all[k] = v
	}
	policy := p.ApprovalPolicy
	if policy == "" {
		policy = all["approval_policy"]
	}
	t := &turn{cwd: p.Cwd, servers: map[string]*fake.MCP{}, never: policy == "never", resumed: resumed}
	for k, value := range all {
		rest, ok := strings.CutPrefix(k, "mcp_servers.")
		name, ok2 := strings.CutSuffix(rest, ".url")
		if !ok2 {
			name, ok2 = strings.CutSuffix(rest, ".command")
		}
		if !ok || !ok2 || strings.Contains(name, ".") {
			continue
		}
		var c *fake.MCP
		var err error
		if strings.HasSuffix(k, ".command") {
			var args []string
			if raw := all["mcp_servers."+name+".args"]; raw != "" && json.Unmarshal([]byte(raw), &args) != nil {
				fmt.Fprintln(os.Stderr, "fake codex: mcp_servers."+name+".args is not a JSON array:", raw)
				continue
			}
			env := map[string]string{}
			for ek, ev := range all {
				if e, ok := strings.CutPrefix(ek, "mcp_servers."+name+".env."); ok {
					env[e] = ev
				}
			}
			c, err = fake.DialStdio(value, args, env)
		} else {
			headers := map[string]string{}
			for hk, hv := range all {
				if h, ok := strings.CutPrefix(hk, "mcp_servers."+name+".http_headers."); ok {
					headers[h] = hv
				}
			}
			if env := all["mcp_servers."+name+".bearer_token_env_var"]; env != "" {
				headers["Authorization"] = "Bearer " + os.Getenv(env)
			}
			c, err = fake.Dial(value, headers)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "fake codex:", err)
			continue
		}
		t.servers[name] = c
	}

	var disabled []string
	var entries []struct {
		Path    string `json:"path"`
		Enabled bool   `json:"enabled"`
	}
	if raw := all["skills.config"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			var decoded struct {
				Config []struct {
					Path    string `json:"path"`
					Enabled bool   `json:"enabled"`
				}
			}
			if _, err := toml.Decode("config="+raw, &decoded); err != nil {
				fmt.Fprintln(os.Stderr, "skills.config:", err)
			}
			entries = decoded.Config
		}
	}
	for _, e := range entries {
		if !e.Enabled {
			disabled = append(disabled, e.Path)
		}
	}
	var err error
	t.skills, err = fake.DiscoverSkills("codex", p.Cwd, extra, disabled)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if err := fake.LogSkills(t.skills); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return t
}

func (t *turn) play(entries []entry, in *bufio.Scanner, log *fake.Log) error {
	started := map[string]item{}
	answer, skip := "", false
	repoReply := ""
	for _, e := range entries {
		switch e.Fake {
		case "platform-create", "platform-edit", "platform-denied", "platform-configure", "platform-import", "platform-skill-import", "platform-read", "platform-bots", "platform-call":
			var err error
			calls := 0
			// Like app-server, each MCP call starts an mcpToolCall item.
			announce := func(name string, args json.RawMessage) error {
				calls++
				b, _ := json.Marshal(map[string]any{"method": "item/started", "params": map[string]any{"item": map[string]any{
					"type": "mcpToolCall", "id": fmt.Sprintf("platform-%d", calls), "server": "atoll", "tool": name,
					"status": "inProgress", "arguments": args}}})
				fmt.Println(string(b))
				return nil
			}
			repoReply, err = fake.PlatformStep(t.servers["atoll"], strings.TrimPrefix(e.Fake, "platform-"), t.userMessage, announce)
			if err != nil {
				return err
			}
			continue
		case "mcp-use":
			var err error
			if repoReply, err = t.mcpUse(in, log); err != nil {
				return err
			}
			continue
		case "skills-use", "skills-hold", "skills-worktree", "skills-run":
			if e.Fake == "skills-worktree" {
				var err error
				t.skills, err = fake.WorktreeSkills(t.servers["atoll"], "codex", t.skills)
				if err != nil {
					return err
				}
			}
			if e.Fake == "skills-hold" {
				if err := fake.HoldSkills(); err != nil {
					return err
				}
			}
			var err error
			if e.Fake == "skills-run" {
				repoReply, err = fake.SkillsRun(t.skills, t.userMessage)
			} else {
				repoReply, err = fake.SkillsReply(t.skills)
			}
			if err != nil {
				return err
			}
			continue
		case "memory-save", "memory-save-user", "memory-recall", "memory-resume", "memory-retire":
			var err error
			repoReply, err = fake.MemoryStep(t.servers["atoll"], strings.TrimPrefix(e.Fake, "memory-"), t.userMessage, nil)
			if err != nil {
				return err
			}
			continue
		case "repo-fix", "repo-question", "repo-uncommitted", "repo-resume":
			var err error
			repoReply, err = fake.RepoStep(t.servers["atoll"], strings.TrimPrefix(e.Fake, "repo-"), t.cwd)
			if err != nil {
				return err
			}
			continue
		case "logins-probe":
			var err error
			if repoReply, err = fake.LoginsProbe(); err != nil {
				return err
			}
			continue
		case "github-probe":
			var err error
			if repoReply, err = fake.GitHubProbe(t.servers["github"]); err != nil {
				return err
			}
			continue
		case "ask":
			if skip {
				continue
			}
			var err error
			if answer, err = t.ask(e.Msg, in, log); err != nil {
				return err
			}
			continue
		case "allow", "deny":
			skip = e.Fake != answer
			continue
		case "hang":
			if !skip {
				fake.Hang()
			}
			continue
		case "pause":
			if !skip {
				fake.Pause(e.MS)
			}
			continue
		}
		if skip {
			continue
		}
		encoded, _ := json.Marshal(repoReply)
		out := json.RawMessage(strings.ReplaceAll(string(e.Msg), "$REPO_REPLY", string(encoded[1:len(encoded)-1])))
		var m message
		_ = json.Unmarshal(out, &m)
		var p itemParams
		_ = json.Unmarshal(m.Params, &p)
		switch {
		case m.Method == "item/started":
			started[p.Item.ID] = p.Item
		case m.Method == "item/completed" && p.Item.Type == "commandExecution":
			time.Sleep(fake.Pace)
		case m.Method == "item/completed" && p.Item.Type == "mcpToolCall":
			time.Sleep(fake.Pace)
			var err error
			if out, err = t.call(out, started[p.Item.ID]); err != nil {
				return err
			}
		}
		fmt.Println(string(out))
	}
	return nil
}

// ask sends the approval request and waits for the client's decision.
func (t *turn) ask(req json.RawMessage, in *bufio.Scanner, log *fake.Log) (string, error) {
	if t.never {
		return "deny", nil
	}
	var r message
	if err := json.Unmarshal(req, &r); err != nil {
		return "", err
	}
	fmt.Println(string(req))
	for in.Scan() {
		if err := log.Stdin(in.Text()); err != nil {
			return "", err
		}
		var m message
		if json.Unmarshal(in.Bytes(), &m) != nil || m.Method != "" || string(m.ID) != string(r.ID) {
			continue
		}
		var res struct {
			Decision json.RawMessage `json:"decision"`
			Action   string          `json:"action"` // an elicitation answers accept, decline or cancel
		}
		if err := json.Unmarshal(m.Result, &res); err != nil || res.Decision == nil && res.Action == "" {
			return "", fmt.Errorf("answer to %s has no decision: %s", r.Method, in.Text())
		}
		var d string
		if res.Decision == nil {
			d = res.Action
		} else if json.Unmarshal(res.Decision, &d) != nil {
			return "allow", nil // {"acceptWithExecpolicyAmendment": ...} and the like
		}
		switch d {
		case "accept", "acceptForSession":
			return "allow", nil
		case "decline", "cancel":
			return "deny", nil
		}
		return "", fmt.Errorf("answer to %s has decision %q", r.Method, d)
	}
	if err := in.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("stdin closed while %s waited for an answer", r.Method)
}

// mcpUse calls each tool the prompt names on the configured server, as app-server would: an
// mcpToolCall item starts, Codex asks the client through an elicitation before a server that is not
// Atoll's runs a tool, then the item completes with the server's answer. A server the config does
// not hold is not called.
func (t *turn) mcpUse(in *bufio.Scanner, log *fake.Log) (string, error) {
	calls, err := fake.MCPCalls(t.userMessage)
	if err != nil {
		return "", err
	}
	var lines []string
	for i, call := range calls {
		c := t.servers[call.Server]
		if c == nil {
			lines = append(lines, fake.MCPLine(call, "unavailable"))
			continue
		}
		it := map[string]any{"type": "mcpToolCall", "id": fmt.Sprintf("mcp-use-%d", i+1), "server": call.Server, "tool": call.Tool,
			"status": "inProgress", "arguments": call.Args, "result": nil, "error": nil, "durationMs": nil}
		emit := func(method string) {
			b, _ := json.Marshal(map[string]any{"method": method, "params": map[string]any{"item": it}})
			fmt.Println(string(b))
		}
		emit("item/started")
		req, _ := json.Marshal(map[string]any{"id": 9000 + i + 1, "method": "mcpServer/elicitation/request", "params": map[string]any{
			"serverName": call.Server, "mode": "form",
			"message":         fmt.Sprintf("Allow the %s MCP server to run tool %q?", call.Server, call.Tool),
			"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"_meta":           map[string]any{"codex_approval_kind": "mcp_tool_call", "tool_title": call.Tool, "tool_params": call.Args},
		}})
		answer, err := t.ask(req, in, log)
		if err != nil {
			return "", err
		}
		time.Sleep(fake.Pace)
		text, isErr := "denied", true
		if answer == "allow" {
			if text, isErr, err = c.Call(call.Tool, call.Args); err != nil {
				return "", err
			}
		}
		it["status"], it["durationMs"] = "completed", 12
		if isErr {
			it["status"], it["error"] = "failed", map[string]string{"message": text}
		} else {
			it["result"] = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "structuredContent": nil}
		}
		emit("item/completed")
		lines = append(lines, fake.MCPLine(call, text))
	}
	return strings.Join(lines, "\n"), nil
}

// call makes the MCP call of a completed mcpToolCall item and puts the real result in it.
func (t *turn) call(completed json.RawMessage, it item) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(completed, &m); err != nil {
		return nil, err
	}
	done := m["params"].(map[string]any)["item"].(map[string]any)
	c := t.servers[it.Server]
	if c == nil {
		done["status"], done["result"] = "failed", nil
		done["error"] = map[string]string{"message": fmt.Sprintf("unknown MCP server '%s'", it.Server)}
		return json.Marshal(m)
	}
	text, isErr, err := c.Call(it.Tool, it.Arguments)
	if err != nil {
		return nil, err
	}
	if isErr {
		done["status"], done["result"] = "failed", nil
		done["error"] = map[string]string{"message": text}
	} else {
		done["result"] = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "structuredContent": nil}
	}
	return json.Marshal(m)
}

// flatten writes v into out as dotted keys: {"a":{"b":"x"}} is a.b=x.
func flatten(prefix string, v any, out map[string]string) {
	obj, ok := v.(map[string]any)
	if !ok {
		if s, ok := v.(string); ok {
			out[prefix] = s
		} else if v != nil {
			b, _ := json.Marshal(v)
			out[prefix] = string(b)
		}
		return
	}
	for k, x := range obj {
		if prefix != "" {
			k = prefix + "." + k
		}
		flatten(k, x, out)
	}
}
