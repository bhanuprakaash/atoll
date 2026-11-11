package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"atoll/e2e/fakes/fake"
)

type (
	// player prints a recording the way claude would have produced it in this process: MCP tool
	// calls really go to the configured servers and permission asks to the prompt tool.
	player struct {
		skills      []fake.SkillCopy
		servers     map[string]*fake.MCP // connected, by name
		failed      []string             // configured servers that did not connect
		prompt      string               // --permission-prompt-tool
		allowed     []string             // --allowedTools
		partial     bool                 // --include-partial-messages: print stream_event lines
		denied      map[string]string    // denial message, by tool_use id
		uses        map[string]toolUse   // tool calls printed so far, by id
		mcpConfig   mcpConfig            // loaded --mcp-config values, after environment expansion
		userMessage string
		session     string // from the init line
		started     bool
	}

	mcpConfig struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}

	mcpServer struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}

	toolUse struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}

	toolResult struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
	}

	// permission is what a permission prompt tool answers.
	permission struct {
		Behavior string `json:"behavior"`
		Message  string `json:"message"`
	}
)

func newPlayer(args []string) (*player, error) {
	p := &player{servers: map[string]*fake.MCP{}, denied: map[string]string{}, uses: map[string]toolUse{}, mcpConfig: mcpConfig{MCPServers: map[string]mcpServer{}}, prompt: flag(args, "--permission-prompt-tool"),
		partial: slices.Contains(args, "--include-partial-messages")}
	for _, name := range []string{"--allowedTools", "--allowed-tools"} {
		for _, v := range values(args, name) {
			p.allowed = append(p.allowed, strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })...)
		}
	}
	for _, v := range values(args, "--mcp-config") {
		src := []byte(v)
		if !strings.HasPrefix(strings.TrimSpace(v), "{") {
			b, err := os.ReadFile(v)
			if err != nil {
				return nil, fmt.Errorf("--mcp-config: %w", err)
			}
			src = b
		}
		var cfg mcpConfig
		if err := json.Unmarshal(src, &cfg); err != nil {
			return nil, fmt.Errorf("--mcp-config %s: %w", v, err)
		}
		for name, s := range cfg.MCPServers {
			s.URL = fake.ExpandEnv(s.URL, os.Getenv)
			for k, v := range s.Headers {
				s.Headers[k] = fake.ExpandEnv(v, os.Getenv)
			}
			for k, v := range s.Env {
				s.Env[k] = fake.ExpandEnv(v, os.Getenv)
			}
			p.mcpConfig.MCPServers[name] = s
			var c *fake.MCP
			var err error
			switch {
			case s.Type == "http" && s.URL != "":
				c, err = fake.Dial(s.URL, s.Headers)
			case (s.Type == "stdio" || s.Type == "") && s.Command != "":
				c, err = fake.DialStdio(s.Command, s.Args, s.Env)
			default:
				p.failed = append(p.failed, name)
				continue
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "fake claude:", err)
				p.failed = append(p.failed, name)
				continue
			}
			p.servers[name] = c
		}
	}
	cwd, _ := os.Getwd()
	var err error
	p.skills, err = fake.DiscoverSkills("claude", cwd, values(args, "--add-dir"), nil)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *player) play(lines []string) error {
	var last *toolUse
	answer, skip := "", false
	repoReply := ""
	for _, s := range lines {
		switch directive := fake.Directive([]byte(s)); directive {
		case "platform-create", "platform-edit", "platform-denied", "platform-configure", "platform-import", "platform-skill-import", "platform-read", "platform-bots", "platform-call", "platform-calls":
			var err error
			repoReply, err = fake.PlatformStep(p.servers["atoll"], strings.TrimPrefix(directive, "platform-"), p.userMessage, func(name string, args json.RawMessage) error {
				answer, err := p.ask(&toolUse{ID: "platform-" + name, Name: "mcp__atoll__" + name, Input: args})
				if err != nil {
					return err
				}
				if answer != "allow" {
					return fmt.Errorf("platform tool denied: %s", name)
				}
				return nil
			})
			if err != nil {
				return err
			}
			continue
		case "mcp-use":
			var err error
			if repoReply, err = p.mcpUse(); err != nil {
				return err
			}
			continue
		case "skills-use", "skills-hold", "skills-worktree", "skills-run":
			if directive == "skills-worktree" {
				var err error
				p.skills, err = fake.WorktreeSkills(p.servers["atoll"], "claude", p.skills)
				if err != nil {
					return err
				}
			}
			if directive == "skills-hold" {
				if err := fake.HoldSkills(); err != nil {
					return err
				}
			}
			var err error
			if directive == "skills-run" {
				repoReply, err = fake.SkillsRun(p.skills, p.userMessage)
			} else {
				repoReply, err = fake.SkillsReply(p.skills)
			}
			if err != nil {
				return err
			}
			continue
		case "memory-save", "memory-save-user", "memory-save-pair", "memory-recall", "memory-resume", "memory-retire":
			var err error
			repoReply, err = fake.MemoryStep(p.servers["atoll"], strings.TrimPrefix(directive, "memory-"), p.userMessage, func(name string, args json.RawMessage) error {
				answer, err := p.ask(&toolUse{ID: "memory-" + name, Name: "mcp__atoll__" + name, Input: args})
				if err != nil {
					return err
				}
				if answer != "allow" {
					return fmt.Errorf("memory tool %s was denied", name)
				}
				return nil
			})
			if err != nil {
				return err
			}
			continue
		case "repo-fix", "repo-question", "repo-uncommitted", "repo-resume", "repo-github-fix":
			var err error
			repoReply, err = fake.RepoStep(p.servers["atoll"], strings.TrimPrefix(directive, "repo-"), "")
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
			if repoReply, err = fake.GitHubProbe(p.servers["github"]); err != nil {
				return err
			}
			continue
		case "procs-hold", "procs-scan":
			var err error
			if directive == "procs-hold" {
				repoReply, err = fake.ProcsHold(p.servers)
			} else {
				repoReply, err = fake.ProcsScan(p.userMessage)
			}
			if err != nil {
				return err
			}
			continue
		case "ask":
			if skip {
				continue
			}
			if last == nil {
				return fmt.Errorf("ask with no tool call before it")
			}
			var err error
			if answer, err = p.ask(last); err != nil {
				return err
			}
			continue
		case "allow", "deny":
			skip = fake.Directive([]byte(s)) != answer
			continue
		case "resumed":
			continue
		case "pause":
			if !skip {
				fake.Pause(fake.PauseMS([]byte(s)))
			}
			continue
		case "hang":
			if !skip {
				fake.Hang()
			}
			continue
		}
		if skip {
			continue
		}
		encoded, _ := json.Marshal(repoReply)
		s = strings.ReplaceAll(s, "$REPO_REPLY", string(encoded[1:len(encoded)-1]))
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			return err
		}
		switch typ := str(m["type"]); {
		case typ == "system" && str(m["subtype"]) == "init" && !p.started:
			p.started = true
			p.session = str(m["session_id"])
			s = p.init(m)
		case typ == "assistant":
			for _, u := range p.toolUses(m) {
				p.uses[u.ID] = u
				last = &u
			}
		case typ == "stream_event" && !p.partial:
			continue
		case typ == "user":
			var err error
			if s, err = p.result(m, s); err != nil {
				return err
			}
		}
		fmt.Println(s)
	}
	return nil
}

// mcpUse calls each tool the prompt names on the configured server, as claude would: it prints the
// tool call, asks the permission prompt tool, then prints the server's answer. A tool the server
// does not list, or a server the config does not hold, is not called.
func (p *player) mcpUse() (string, error) {
	calls, err := fake.MCPCalls(p.userMessage)
	if err != nil {
		return "", err
	}
	var lines []string
	for i, call := range calls {
		c := p.servers[call.Server]
		if c == nil || !c.Tools[call.Tool] {
			lines = append(lines, fake.MCPLine(call, "unavailable"))
			continue
		}
		use := toolUse{Type: "tool_use", ID: fmt.Sprintf("toolu_mcp_use_%d", i+1), Name: "mcp__" + call.Server + "__" + call.Tool, Input: call.Args}
		p.uses[use.ID] = use
		emit := func(l map[string]any) {
			l["parent_tool_use_id"], l["session_id"] = nil, p.session
			b, _ := json.Marshal(l)
			fmt.Println(string(b))
		}
		emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "type": "message", "content": []toolUse{use}}})
		answer, err := p.ask(&use)
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
		result := map[string]any{"type": "tool_result", "tool_use_id": use.ID, "content": []map[string]string{{"type": "text", "text": text}}}
		if isErr {
			result["is_error"] = true
		}
		emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{result}}})
		lines = append(lines, fake.MCPLine(call, text))
	}
	return strings.Join(lines, "\n"), nil
}

// init reports the configured MCP servers as connected or failed.
func (p *player) init(m map[string]json.RawMessage) string {
	var servers []map[string]string
	for name := range p.servers {
		servers = append(servers, map[string]string{"name": name, "status": "connected"})
	}
	for _, name := range p.failed {
		servers = append(servers, map[string]string{"name": name, "status": "failed"})
	}
	if servers == nil {
		servers = []map[string]string{}
	}
	m["mcp_servers"], _ = json.Marshal(servers)
	b, _ := json.Marshal(m)
	return string(b)
}

func (p *player) toolUses(m map[string]json.RawMessage) []toolUse {
	var msg struct {
		Content []toolUse `json:"content"`
	}
	_ = json.Unmarshal(m["message"], &msg)
	var out []toolUse
	for _, c := range msg.Content {
		if c.Type == "tool_use" {
			out = append(out, c)
		}
	}
	return out
}

// ask returns "allow" or "deny" for a tool call, as claude decides it.
func (p *player) ask(t *toolUse) (string, error) {
	if p.isAllowed(t.Name) {
		return "allow", nil
	}
	server, tool, ok := mcpTool(p.prompt)
	c := p.servers[server]
	if !ok || c == nil {
		p.denied[t.ID] = fmt.Sprintf("Claude requested permissions to use %s, but you haven't granted it yet.", t.Name)
		return "deny", nil
	}
	args, _ := json.Marshal(map[string]any{"tool_name": t.Name, "input": t.Input, "tool_use_id": t.ID})
	out, _, err := c.Call(tool, args)
	if err != nil {
		return "", err
	}
	var perm permission
	if err := json.Unmarshal([]byte(out), &perm); err != nil {
		return "", fmt.Errorf("permission prompt tool %s answered %q: want {\"behavior\":\"allow\"|\"deny\",...}", p.prompt, out)
	}
	switch perm.Behavior {
	case "allow":
		return "allow", nil
	case "deny":
		p.denied[t.ID] = perm.Message
		return "deny", nil
	}
	return "", fmt.Errorf("permission prompt tool %s answered behavior %q", p.prompt, perm.Behavior)
}

func (p *player) isAllowed(name string) bool {
	for _, a := range p.allowed {
		switch {
		case a == name,
			strings.HasSuffix(a, "__*") && strings.HasPrefix(name, strings.TrimSuffix(a, "*")),
			strings.HasPrefix(a, "mcp__") && strings.Count(a, "__") == 1 && strings.HasPrefix(name, a+"__"):
			return true
		}
	}
	return false
}

// result takes the time a tool runs, then gives the tool result of this run: a denial, or what
// the MCP server really answered.
func (p *player) result(m map[string]json.RawMessage, s string) (string, error) {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(m["message"], &msg)
	var results []toolResult
	if json.Unmarshal(msg.Content, &results) != nil || len(results) == 0 || results[0].Type != "tool_result" {
		return s, nil
	}
	time.Sleep(fake.Pace)
	id := results[0].ToolUseID
	if reason, ok := p.denied[id]; ok {
		return replaceResult(m, reason, true), nil
	}
	use := p.uses[id]
	server, tool, ok := mcpTool(use.Name)
	if !ok {
		return s, nil
	}
	c := p.servers[server]
	if c == nil {
		return replaceResult(m, fmt.Sprintf("No such tool available: %s", use.Name), true), nil
	}
	out, isErr, err := c.Call(tool, use.Input)
	if err != nil {
		return "", err
	}
	return replaceResult(m, out, isErr), nil
}

func replaceResult(m map[string]json.RawMessage, text string, isErr bool) string {
	var msg map[string]json.RawMessage
	_ = json.Unmarshal(m["message"], &msg)
	var results []map[string]any
	_ = json.Unmarshal(msg["content"], &results)
	results[0]["content"] = []map[string]string{{"type": "text", "text": text}}
	if isErr {
		results[0]["is_error"] = true
	}
	msg["content"], _ = json.Marshal(results)
	m["message"], _ = json.Marshal(msg)
	b, _ := json.Marshal(m)
	return string(b)
}

// mcpTool splits mcp__<server>__<tool>.
func mcpTool(name string) (server, tool string, ok bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	return strings.Cut(rest, "__")
}

func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// values returns every value of a flag: each argument after it up to the next flag.
func values(args []string, name string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if value, ok := strings.CutPrefix(args[i], name+"="); ok {
			out = append(out, value)
			continue
		}
		if args[i] != name {
			continue
		}
		for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			out = append(out, args[i])
		}
	}
	return slices.Clip(out)
}
