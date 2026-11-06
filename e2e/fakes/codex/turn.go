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
	for k, url := range all {
		rest, ok := strings.CutPrefix(k, "mcp_servers.")
		name, ok2 := strings.CutSuffix(rest, ".url")
		if !ok || !ok2 || strings.Contains(name, ".") {
			continue
		}
		headers := map[string]string{}
		for hk, hv := range all {
			if h, ok := strings.CutPrefix(hk, "mcp_servers."+name+".http_headers."); ok {
				headers[h] = hv
			}
		}
		if env := all["mcp_servers."+name+".bearer_token_env_var"]; env != "" {
			headers["Authorization"] = "Bearer " + os.Getenv(env)
		}
		c, err := fake.Dial(url, headers)
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
		case "platform-create", "platform-edit", "platform-denied", "platform-configure", "platform-import":
			var err error
			repoReply, err = fake.PlatformStep(t.servers["atoll"], strings.TrimPrefix(e.Fake, "platform-"), t.userMessage, nil)
			if err != nil {
				return err
			}
			continue
		case "skills-use", "skills-hold", "skills-worktree":
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
			repoReply, err = fake.SkillsReply(t.skills)
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
		}
		if err := json.Unmarshal(m.Result, &res); err != nil || res.Decision == nil {
			return "", fmt.Errorf("answer to %s has no decision: %s", r.Method, in.Text())
		}
		var d string
		if json.Unmarshal(res.Decision, &d) != nil {
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
