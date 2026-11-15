package e2e

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Prompts and answers of the recordings in testdata/claude and testdata/codex.
const (
	_skillsPrompt   = "List your skills and MCP servers. Name every one you have; if you have none of a kind, say none."
	_factPrompt     = "Remember this fact: the codeword is heron. Reply with just: ok"
	_recallPrompt   = "What is the codeword? Reply with just the word."
	_claudeSkills   = "**MCP servers:** none. I have no `mcp__*` tools loaded, and the deferred tools (CronCreate, DesignSync, WebFetch, WebSearch, and others) are built-in harness tools, not MCP servers."
	_codexSkills    = "- Skills: none.\n- MCP servers: none connected or exposed."
	_claudeSkillsID = "d662d248-7813-41a1-b832-28f02d5f27aa"
	_claudeFactID   = "567fd9ca-fc18-4a32-a242-a6e148914e80"
	_codexThreadID  = "01a1a5b5-3b2c-71f2-84d9-e7c08190f545"
)

// _wakeWindow is ATOLL_WAKE_WINDOW on every machine: short, so a helper's report wakes its boss
// soon. A test of the window sets its own.
const _wakeWindow = "200ms"

// machine is a user's machine atoll runs on. It sees nothing of the real one: HOME, its data
// dir and the folders provider discovery searches are all temporary.
type machine struct {
	home string   // HOME
	data string   // ATOLL_DATA_DIR
	bin  string   // ATOLL_PROVIDER_PATH: the fakes installed
	path string   // PATH
	env  []string // extra server env for Slice 3
}

// call is one run of a fake provider, from its log.
type call struct {
	pid   int
	cwd   string // as getcwd reports it: symlinks resolved
	argv  []string
	env   map[string]string
	dirs  map[string]fake.Dir
	mcp   json.RawMessage
	stdin []string
}

func newMachine(t *testing.T, providers ...string) *machine {
	t.Helper()
	m := &machine{home: t.TempDir(), data: t.TempDir(), bin: t.TempDir(), env: []string{"ATOLL_WAKE_WINDOW=" + _wakeWindow}}
	m.path = m.bin + ":/usr/bin:/bin"
	if _keychains != "" {
		if err := os.Mkdir(filepath.Join(m.home, "Library"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(_keychains, filepath.Join(m.home, "Library", "Keychains")); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range providers {
		installFake(t, p, m.bin)
	}
	return m
}

// installFake copies the fake provider into dir. A copy, not a link: the fake logs next to its
// own executable, so each machine gets its own log.
func installFake(t *testing.T, provider, dir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(_fakes, provider))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, provider), b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// providerDir is the Atoll-owned settings folder of provider.
func (m *machine) providerDir(provider string) string {
	return filepath.Join(m.data, "providers", provider)
}

// signIn leaves what the fake provider takes as a login in its Atoll folder.
func (m *machine) signIn(t *testing.T, provider string) {
	t.Helper()
	file := map[string]string{"claude": ".credentials.json", "codex": "auth.json"}[provider]
	writeFile(t, filepath.Join(m.providerDir(provider), file), "{}")
}

// calls returns every run of the fake provider but title sessions, in the order they started.
func (m *machine) calls(t *testing.T, provider string) []*call {
	t.Helper()
	return slices.DeleteFunc(m.runs(t, provider), isTitle)
}

// titles returns the title sessions of the fake provider, in the order they started.
func (m *machine) titles(t *testing.T, provider string) []*call {
	t.Helper()
	return slices.DeleteFunc(m.runs(t, provider), func(c *call) bool { return !isTitle(c) })
}

func isTitle(c *call) bool {
	return slices.ContainsFunc(c.argv, func(a string) bool { return strings.HasPrefix(a, fake.TitleSystem) }) ||
		slices.ContainsFunc(c.stdin, func(l string) bool {
			return strings.Contains(l, `"developerInstructions":"`+fake.TitleSystem)
		})
}

// runs returns every run of the fake provider, in the order they started.
func (m *machine) runs(t *testing.T, provider string) []*call {
	t.Helper()
	f, err := os.Open(filepath.Join(m.bin, provider+".log"))
	if err != nil {
		t.Fatalf("%s was never run: %v", provider, err)
	}
	defer func() { _ = f.Close() }()
	var calls []*call
	byPID := map[int]*call{}
	s := bufio.NewScanner(f)
	s.Buffer(nil, 64*1024*1024)
	for s.Scan() {
		var r struct {
			PID   int                 `json:"pid"`
			Cwd   string              `json:"cwd"`
			Argv  []string            `json:"argv"`
			Env   []string            `json:"env"`
			Dirs  map[string]fake.Dir `json:"dirs"`
			MCP   json.RawMessage     `json:"mcpConfig"`
			Stdin string              `json:"stdin"`
		}
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		c, ok := byPID[r.PID]
		if !ok {
			c = &call{pid: r.PID, cwd: r.Cwd, argv: r.Argv, env: map[string]string{}, dirs: r.Dirs}
			for _, kv := range r.Env {
				k, v, _ := strings.Cut(kv, "=")
				c.env[k] = v
			}
			byPID[r.PID] = c
			calls = append(calls, c)
			continue
		}
		if r.MCP != nil {
			c.mcp = r.MCP
			continue
		}
		c.stdin = append(c.stdin, r.Stdin)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return calls
}

// sessions returns the runs of the fake that held a session: claude -p, codex app-server, given
// a user turn. A model-list probe (Slice 6) starts the same mode without one and is not a session.
func (m *machine) sessions(t *testing.T, provider string) []*call {
	t.Helper()
	mode := map[string]string{"claude": "-p", "codex": "app-server"}[provider]
	var out []*call
	for _, c := range m.calls(t, provider) {
		if slices.Contains(c.argv, mode) && slices.ContainsFunc(c.stdin, isTurn) {
			out = append(out, c)
		}
	}
	return out
}

// isTurn reports whether a stdin line gives the provider a user turn.
func isTurn(line string) bool {
	var l struct {
		Type   string `json:"type"`
		Method string `json:"method"`
	}
	return json.Unmarshal([]byte(line), &l) == nil && (l.Type == "user" || l.Method == "turn/start")
}

// assertClaudeIsolated checks the flags and env docs/design.md → Providers gives every run.
func (m *machine) assertClaudeIsolated(t *testing.T, c *call) {
	t.Helper()
	for flag, want := range map[string]string{"--input-format": "stream-json", "--output-format": "stream-json"} {
		if got := flagValue(c.argv, flag); got != want {
			t.Errorf("claude %s %q, want %q; argv %q", flag, got, want, c.argv)
		}
	}
	if !slices.Contains(c.argv, "--strict-mcp-config") {
		t.Errorf("claude argv has no --strict-mcp-config: %q", c.argv)
	}
	if got := flagValue(c.argv, "--setting-sources"); got != "project" {
		t.Errorf("claude --setting-sources %q, want project; argv %q", got, c.argv)
	}
	for k, want := range map[string]string{
		"ENABLE_CLAUDEAI_MCP_SERVERS": "false",
		"CLAUDE_CONFIG_DIR":           m.providerDir("claude"),
		"HOME":                        m.home,
	} {
		if got := c.env[k]; got != want {
			t.Errorf("claude env %s=%q, want %q", k, got, want)
		}
	}
	file := filepath.Join(m.providerDir("claude"), "settings.json")
	if !m.claudeSetting(c, func(s claudeSettings) bool { return s.DisableAllHooks }) {
		t.Errorf(`claude hooks are on: want "disableAllHooks": true in --settings or in %s; argv %q`, file, c.argv)
	}
	if c.env["CLAUDE_CODE_DISABLE_BUNDLED_SKILLS"] != "1" &&
		!m.claudeSetting(c, func(s claudeSettings) bool { return s.DisableBundledSkills }) {
		t.Errorf(`claude bundled skills are on: want "disableBundledSkills": true in --settings or in %s, `+
			`or env CLAUDE_CODE_DISABLE_BUNDLED_SKILLS=1; argv %q`, file, c.argv)
	}
	for _, name := range []string{"design", "doctor", "plugin-authoring"} {
		if !m.claudeSetting(c, func(s claudeSettings) bool { return s.SkillOverrides[name] == "off" }) {
			t.Errorf(`claude bundled skill %q survives disableBundledSkills: want "skillOverrides": {%q: "off"} `+
				`in --settings or in %s; argv %q`, name, name, file, c.argv)
		}
	}
}

type claudeSettings struct {
	DisableAllHooks      bool              `json:"disableAllHooks"`
	DisableBundledSkills bool              `json:"disableBundledSkills"`
	SkillOverrides       map[string]string `json:"skillOverrides"`
}

// claudeSetting reports whether ok holds for the --settings claude got (JSON or a file) or for
// the settings.json of its Atoll folder.
func (m *machine) claudeSetting(c *call, ok func(claudeSettings) bool) bool {
	var sources [][]byte
	if v := flagValue(c.argv, "--settings"); strings.HasPrefix(strings.TrimSpace(v), "{") {
		sources = append(sources, []byte(v))
	} else if v != "" {
		b, _ := os.ReadFile(v)
		sources = append(sources, b)
	}
	b, _ := os.ReadFile(filepath.Join(m.providerDir("claude"), "settings.json"))
	sources = append(sources, b)
	for _, src := range sources {
		var s claudeSettings
		if json.Unmarshal(src, &s) == nil && ok(s) {
			return true
		}
	}
	return false
}

// assertCodexIsolated checks the env and config docs/design.md → Providers gives every run.
// Config counts from `-c key=value` arguments and from the config of the thread request.
func (m *machine) assertCodexIsolated(t *testing.T, c *call, method string) {
	t.Helper()
	for k, want := range map[string]string{"CODEX_HOME": m.providerDir("codex"), "HOME": m.home} {
		if got := c.env[k]; got != want {
			t.Errorf("codex env %s=%q, want %q", k, got, want)
		}
	}
	cfg := map[string]string{}
	for i, a := range c.argv {
		if (a == "-c" || a == "--config") && i+1 < len(c.argv) {
			k, v, _ := strings.Cut(c.argv[i+1], "=")
			cfg[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if r := request(c, method); r != nil {
		flatten("", r.Config, cfg)
	} else {
		t.Errorf("codex got no %s; stdin:\n%s", method, strings.Join(c.stdin, "\n"))
	}
	for _, k := range []string{
		"skills.bundled.enabled", "features.apps", "features.plugins", "features.remote_plugin",
	} {
		if cfg[k] != "false" {
			t.Errorf("codex config %s=%q, want false; argv %q", k, cfg[k], c.argv)
		}
	}
}

type threadRequest struct {
	ThreadID string         `json:"threadId"`
	Config   map[string]any `json:"config"`
}

// request returns the params of the first JSON-RPC request with method on c's stdin.
func request(c *call, method string) *threadRequest {
	for _, line := range c.stdin {
		var m struct {
			Method string        `json:"method"`
			Params threadRequest `json:"params"`
		}
		if json.Unmarshal([]byte(line), &m) == nil && m.Method == method {
			return &m.Params
		}
	}
	return nil
}

// flatten writes v into out as dotted keys: {"a":{"b":false}} is a.b=false.
func flatten(prefix string, v any, out map[string]string) {
	obj, ok := v.(map[string]any)
	if !ok {
		b, _ := json.Marshal(v)
		out[prefix] = string(b)
		return
	}
	for k, x := range obj {
		if prefix != "" {
			k = prefix + "." + k
		}
		flatten(k, x, out)
	}
}

func flagValue(argv []string, flag string) string {
	i := slices.Index(argv, flag)
	if i < 0 || i+1 >= len(argv) {
		return ""
	}
	return argv[i+1]
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
