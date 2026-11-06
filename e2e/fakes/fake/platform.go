package fake

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// GreetingSkill keeps Claude-only controls in canonical text to exercise translation.
const GreetingSkill = "---\nname: reef-greeting\ndescription: Give the Reef greeting.\nallowed-tools: Read\ncontext: fork\n---\nHello from the lagoon.\n"

// PlatformTools is the complete capability set that non-platform runs must never receive.
func PlatformTools() []string {
	return []string{"create_bot", "edit_bot", "create_skill", "edit_skill", "register_repo", "attach_connection"}
}

// PlatformStep uses ids from actual MCP results rather than assumed product state.
func PlatformStep(c *MCP, mode, prompt string, before func(string, json.RawMessage) error) (string, error) {
	if c == nil {
		return "", fmt.Errorf("atoll MCP server absent")
	}
	if mode == "denied" {
		for _, name := range PlatformTools() {
			if c.Tools[name] {
				return "", fmt.Errorf("non-platform Bot received %s", name)
			}
		}
		if text, bad, err := c.Call("create_bot", json.RawMessage(`{"name":"Forbidden","persona":"No access.","provider":"claude"}`)); err == nil && !bad {
			return "", fmt.Errorf("non-platform guessed create_bot succeeded: %s", text)
		}
		return "No platform tools available.", nil
	}
	for _, name := range PlatformTools() {
		if !c.Tools[name] {
			return "", fmt.Errorf("platform tool missing: %s", name)
		}
	}
	if mode == "call" {
		return callOne(c, prompt, before)
	}
	call := func(name string, args any) (map[string]any, error) {
		b, _ := json.Marshal(args)
		if before != nil {
			if err := before(name, b); err != nil {
				return nil, err
			}
		}
		text, bad, err := c.Call(name, b)
		if err != nil {
			return nil, err
		}
		if bad {
			return nil, fmt.Errorf("%s: %s", name, text)
		}
		var out map[string]any
		err = json.Unmarshal([]byte(text), &out)
		return out, err
	}

	if mode == "configure" {
		_, text, ok := strings.Cut(prompt, "Configure platform fixtures. ")
		if !ok {
			return "", fmt.Errorf("configure fixture payload absent")
		}
		var cfg struct {
			BotID        string `json:"bot_id"`
			SkillID      string `json:"skill_id"`
			RepoPath     string `json:"repo_path"`
			ConnectionID string `json:"connection_id"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &cfg); err != nil {
			return "", err
		}
		updated := strings.ReplaceAll(GreetingSkill, "Hello from the lagoon.", "Hello from the reef.")
		out, err := call("edit_skill", map[string]any{"id": cfg.SkillID, "content": updated})
		if err != nil {
			return "", err
		}
		if out["id"] != cfg.SkillID || out["content"] != updated {
			return "", fmt.Errorf("edit_skill result: %v", out)
		}
		out, err = call("register_repo", map[string]any{"name": "chief-repo", "path": cfg.RepoPath})
		if err != nil {
			return "", err
		}
		if out["id"] == nil || out["path"] != cfg.RepoPath {
			return "", fmt.Errorf("register_repo result: %v", out)
		}
		out, err = call("attach_connection", map[string]any{"bot_id": cfg.BotID, "connection_id": cfg.ConnectionID})
		if err != nil {
			return "", err
		}
		connections, _ := out["connections"].([]any)
		if out["id"] != cfg.BotID || len(connections) != 1 || connections[0] != cfg.ConnectionID {
			return "", fmt.Errorf("attach_connection result: %v", out)
		}
		return "Edited the skill, registered the repo and attached the connection.", nil
	}
	if mode == "edit" {
		_, id, _ := strings.Cut(prompt, "bot_id=")
		fields := strings.Fields(id)
		if len(fields) == 0 {
			return "", fmt.Errorf("edit scenario requires bot_id")
		}
		id = fields[0]
		out, err := call("edit_bot", map[string]any{"id": id, "persona": "A lagoon guide."})
		if err != nil {
			return "", err
		}
		if out["id"] != id || out["persona"] != "A lagoon guide." {
			return "", fmt.Errorf("edit_bot result: %v", out)
		}
		return "Updated the Bot persona.", nil
	}
	out, err := call("create_skill", map[string]any{"name": "reef-greeting", "content": GreetingSkill})
	if err != nil {
		return "", err
	}
	id, _ := out["id"].(string)
	if id == "" || out["content"] != GreetingSkill {
		return "", fmt.Errorf("create_skill result: %v", out)
	}
	provider := "claude"
	if strings.Contains(prompt, "using codex") {
		provider = "codex"
	}
	out, err = call("create_bot", map[string]any{"name": "Lagoon", "persona": "A lagoon guide.", "provider": provider, "skills": []string{id}})
	if err != nil {
		return "", err
	}
	if out["id"] == nil || out["name"] != "Lagoon" {
		return "", fmt.Errorf("create_bot result: %v", out)
	}
	return "Created reef-greeting and Lagoon.", nil
}

// callOne makes the one call the prompt names after "Call a platform tool. " as
// {"tool":...,"args":{...}}, sending only arguments the tool's schema declares, as a model reading
// tools/list would. Its reply is the result text, or "Error: " and the error text.
func callOne(c *MCP, prompt string, before func(string, json.RawMessage) error) (string, error) {
	_, payload, ok := strings.Cut(prompt, "Call a platform tool. ")
	if !ok {
		return "", fmt.Errorf("call payload absent")
	}
	var in struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &in); err != nil {
		return "", err
	}
	var args map[string]any
	if err := json.Unmarshal(in.Args, &args); err != nil {
		return "", err
	}
	for name := range args {
		if !slices.Contains(c.Params[in.Tool], name) {
			return "", fmt.Errorf("%s declares no %q parameter: %q", in.Tool, name, c.Params[in.Tool])
		}
	}
	if before != nil {
		if err := before(in.Tool, in.Args); err != nil {
			return "", err
		}
	}
	text, bad, err := c.Call(in.Tool, in.Args)
	if err != nil {
		return "Error: " + errors.Unwrap(err).Error(), nil
	}
	if bad {
		return "Error: " + text, nil
	}
	return text, nil
}
