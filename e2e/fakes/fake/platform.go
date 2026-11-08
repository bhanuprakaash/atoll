package fake

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	if mode == "skill-import" && !c.Tools["create_bot"] {
		return deniedImport(c)
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
	if mode == "call" {
		return callOne(c, prompt, before)
	}
	if mode == "calls" {
		return callMany(c, prompt, before)
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
	if mode == "import" {
		return importAgent(call, prompt)
	}
	if mode == "skill-import" {
		return importSkill(c, raw, prompt)
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

var (
	_agentFile  = regexp.MustCompile(`~/\.claude/agents/[^\s/]+\.md`)
	_localSkill = regexp.MustCompile(`~/\.claude/skills/([^\s/]+)/SKILL\.md`)
)

// importAgent reads the agent file and its local skills from HOME as the provider's own file
// tool would, then imports each skill's SKILL.md and creates the Bot without any ~/.claude path.
func importAgent(call func(string, any) (map[string]any, error), prompt string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := _agentFile.FindString(prompt)
	if path == "" {
		return "", fmt.Errorf("import scenario requires ~/.claude/agents/<agent>.md")
	}
	b, err := os.ReadFile(filepath.Join(home, strings.TrimPrefix(path, "~/")))
	if err != nil {
		return "", err
	}
	front, body, ok := strings.Cut(strings.TrimPrefix(string(b), "---\n"), "---\n")
	if !ok {
		return "", fmt.Errorf("agent file has no frontmatter: %s", path)
	}
	name := ""
	for line := range strings.Lines(front) {
		if value, ok := strings.CutPrefix(line, "name:"); ok {
			name = strings.TrimSpace(value)
		}
	}
	persona := strings.TrimSpace(body)
	var ids, names []string
	for _, m := range _localSkill.FindAllStringSubmatch(body, -1) {
		content, err := os.ReadFile(filepath.Join(home, ".claude", "skills", m[1], "SKILL.md"))
		if err != nil {
			return "", err
		}
		out, err := call("create_skill", map[string]any{"name": m[1], "content": string(content)})
		if err != nil {
			return "", err
		}
		id, _ := out["id"].(string)
		if id == "" || out["content"] != string(content) {
			return "", fmt.Errorf("create_skill result: %v", out)
		}
		ids, names = append(ids, id), append(names, m[1])
		persona = strings.ReplaceAll(persona, m[0], "the "+m[1]+" skill")
	}
	provider := "claude"
	if strings.Contains(prompt, "using codex") {
		provider = "codex"
	}
	out, err := call("create_bot", map[string]any{"name": name, "persona": persona, "provider": provider, "skills": ids})
	if err != nil {
		return "", err
	}
	if out["id"] == nil || out["name"] != name {
		return "", fmt.Errorf("create_bot result: %v", out)
	}
	return "Imported " + strings.Join(names, ", ") + " and created " + name + ".", nil
}

// deniedImport is a Bot without the platform switch guessing import_skill: it must not be listed
// and the call must fail.
func deniedImport(c *MCP) (string, error) {
	if c.Tools["import_skill"] {
		return "", fmt.Errorf("non-platform Bot received import_skill")
	}
	if text, bad, err := c.Call("import_skill", json.RawMessage(`{"path":"/"}`)); err == nil && !bad {
		return "", fmt.Errorf("non-platform guessed import_skill succeeded: %s", text)
	}
	return "import_skill denied.", nil
}

// importSkill imports the folder the prompt names after "the skill folder " with import_skill,
// sending replace when the user says "Yes, replace". The reply is a sentence naming the status
// and what was skipped, or what the user must confirm, then the result as one line of JSON.
func importSkill(c *MCP, raw func(string, any) (string, error), prompt string) (string, error) {
	_, rest, ok := strings.Cut(prompt, "the skill folder ")
	fields := strings.Fields(rest)
	if !ok || len(fields) == 0 {
		return "", fmt.Errorf("import scenario requires the skill folder <path>")
	}
	args := map[string]any{"path": fields[0]}
	if strings.Contains(prompt, "Yes, replace") {
		args["replace"] = true
	}
	if !c.Tools["import_skill"] {
		return "", fmt.Errorf("platform tool missing: import_skill")
	}
	for name := range args {
		if !slices.Contains(c.Params["import_skill"], name) {
			return "", fmt.Errorf("import_skill declares no %q parameter: %q", name, c.Params["import_skill"])
		}
	}
	text, err := raw("import_skill", args)
	if err != nil {
		return "", err
	}
	var res struct {
		Status  string   `json:"status"`
		Added   []string `json:"added"`
		Changed []string `json:"changed"`
		Removed []string `json:"removed"`
		Skipped []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"skipped"`
		Skill struct {
			Name string `json:"name"`
		} `json:"skill"`
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return "", fmt.Errorf("import_skill result %q: %w", text, err)
	}
	list := func(items []string) string {
		if len(items) == 0 {
			return "none"
		}
		return strings.Join(items, ", ")
	}
	var summary string
	if res.Status == "confirm" {
		summary = fmt.Sprintf("Import needs confirmation: %s Added: %s. Changed: %s. Removed: %s.",
			res.Warning, list(res.Added), list(res.Changed), list(res.Removed))
	} else {
		skipped := make([]string, 0, len(res.Skipped))
		for _, s := range res.Skipped {
			skipped = append(skipped, s.Path+" ("+s.Reason+")")
		}
		summary = fmt.Sprintf("Import %s: %s. Skipped: %s.", res.Status, res.Skill.Name, list(skipped))
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err != nil {
		return "", err
	}
	return strings.Join(strings.Fields(summary), " ") + "\n" + compact.String(), nil
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

// callMany makes each call of the JSON array the prompt names after "Call platform tools. ", in
// one run, as callOne makes one. Its reply counts the calls.
func callMany(c *MCP, prompt string, before func(string, json.RawMessage) error) (string, error) {
	_, payload, ok := strings.Cut(prompt, "Call platform tools. ")
	if !ok {
		return "", fmt.Errorf("calls payload absent")
	}
	var calls []json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &calls); err != nil {
		return "", err
	}
	for _, call := range calls {
		text, err := callOne(c, "Call a platform tool. "+string(call), before)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(text, "Error: ") {
			return "", fmt.Errorf("%s: %s", call, text)
		}
	}
	return fmt.Sprintf("Made %d platform calls.", len(calls)), nil
}
