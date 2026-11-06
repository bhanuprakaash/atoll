package fake

import (
	"encoding/json"
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

// ReadTools are the platform tools that only read; non-platform runs must never receive them either.
func ReadTools() []string {
	return []string{"list_bots", "list_skills", "list_connections", "list_repos", "list_models"}
}

// PlatformStep uses ids from actual MCP results rather than assumed product state.
func PlatformStep(c *MCP, mode, prompt string, before func(string, json.RawMessage) error) (string, error) {
	if c == nil {
		return "", fmt.Errorf("atoll MCP server absent")
	}
	if mode == "denied" {
		for _, name := range append(PlatformTools(), ReadTools()...) {
			if c.Tools[name] {
				return "", fmt.Errorf("non-platform Bot received %s", name)
			}
		}
		if text, bad, err := c.Call("create_bot", json.RawMessage(`{"name":"Forbidden","persona":"No access.","provider":"claude"}`)); err == nil && !bad {
			return "", fmt.Errorf("non-platform guessed create_bot succeeded: %s", text)
		}
		return "No platform tools available.", nil
	}
	want := PlatformTools()
	if mode == "read" || mode == "bots" {
		want = append(want, ReadTools()...)
	}
	for _, name := range want {
		if !c.Tools[name] {
			return "", fmt.Errorf("platform tool missing: %s", name)
		}
	}
	raw := func(name string, args any) (string, error) {
		b, _ := json.Marshal(args)
		if before != nil {
			if err := before(name, b); err != nil {
				return "", err
			}
		}
		text, bad, err := c.Call(name, b)
		if err != nil {
			return "", err
		}
		if bad {
			return "", fmt.Errorf("%s: %s", name, text)
		}
		return text, nil
	}
	call := func(name string, args any) (map[string]any, error) {
		text, err := raw(name, args)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		err = json.Unmarshal([]byte(text), &out)
		return out, err
	}

	// The reply is every read tool's result by tool name, so a test can compare them with REST.
	if mode == "read" {
		results := map[string]json.RawMessage{}
		for _, name := range ReadTools() {
			text, err := raw(name, map[string]any{})
			if err != nil {
				return "", err
			}
			if !json.Valid([]byte(text)) {
				return "", fmt.Errorf("%s result is not JSON: %s", name, text)
			}
			results[name] = json.RawMessage(text)
		}
		b, err := json.Marshal(results)
		return string(b), err
	}
	if mode == "bots" {
		text, err := raw("list_bots", map[string]any{})
		if err != nil {
			return "", err
		}
		var bots []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(text), &bots); err != nil {
			return "", fmt.Errorf("list_bots result %s: %w", text, err)
		}
		names := make([]string, 0, len(bots))
		for _, b := range bots {
			names = append(names, b.Name)
		}
		slices.Sort(names)
		return "Bots: " + strings.Join(names, ", ") + ".", nil
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
