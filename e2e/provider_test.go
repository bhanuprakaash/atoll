package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	argv  []string
	env   map[string]string
	dirs  map[string]fake.Dir
	stdin []string
}

func newMachine(t *testing.T, providers ...string) *machine {
	t.Helper()
	m := &machine{home: t.TempDir(), data: t.TempDir(), bin: t.TempDir()}
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

func (m *machine) atoll(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, _atoll, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"HOME=" + m.home,
		"PATH=" + m.path,
		"ATOLL_DATA_DIR=" + m.data,
		"ATOLL_PROVIDER_PATH=" + m.bin,
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("atoll %q: %v\nstdout:\n%s\nstderr:\n%s", args, err, out, stderr.String())
	}
	return string(out)
}

// run is `atoll provider run`; it returns the reply and the session id.
func (m *machine) run(t *testing.T, args ...string) (reply, session string) {
	t.Helper()
	out := strings.TrimRight(m.atoll(t, append([]string{"provider", "run"}, args...)...), "\n")
	i := strings.LastIndex(out, "\n")
	reply, last := out[:max(i, 0)], out[i+1:]
	session, ok := strings.CutPrefix(last, "session: ")
	if !ok || i < 0 {
		t.Fatalf("atoll provider run %q: want the reply, then a last line \"session: <id>\"; got:\n%s", args, out)
	}
	return strings.TrimSpace(reply), session
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
			Argv  []string            `json:"argv"`
			Env   []string            `json:"env"`
			Dirs  map[string]fake.Dir `json:"dirs"`
			Stdin string              `json:"stdin"`
		}
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		c, ok := byPID[r.PID]
		if !ok {
			c = &call{pid: r.PID, argv: r.Argv, env: map[string]string{}, dirs: r.Dirs}
			for _, kv := range r.Env {
				k, v, _ := strings.Cut(kv, "=")
				c.env[k] = v
			}
			byPID[r.PID] = c
			calls = append(calls, c)
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

func TestProviderStatus_ListsBoth(t *testing.T) {
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	// The user's own Codex is signed in; Atoll's Codex folder is not.
	writeFile(t, filepath.Join(m.home, ".codex", "auth.json"), "{}")

	got := m.atoll(t, "provider", "status")

	want := "claude\t" + filepath.Join(m.bin, "claude") + "\t2.1.306\tsigned in\n" +
		"codex\t" + filepath.Join(m.bin, "codex") + "\t0.166.0\tsigned out\n"
	if got != want {
		t.Errorf("atoll provider status printed\n%q\nwant\n%q", got, want)
	}
}

func TestProviderStatus_NotInstalled(t *testing.T) {
	m := newMachine(t, "claude")
	// A codex on PATH and in an install folder, but not on ATOLL_PROVIDER_PATH, is not found.
	other := t.TempDir()
	installFake(t, "codex", other)
	installFake(t, "codex", filepath.Join(m.home, ".local", "bin"))
	m.path = other + ":" + m.path

	got := m.atoll(t, "provider", "status")

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "claude\t"+filepath.Join(m.bin, "claude")+"\t") ||
		lines[1] != "codex\tnot installed" {
		t.Errorf("atoll provider status printed\n%q\nwant a claude line, then %q", got, "codex\tnot installed")
	}
}

func TestProviderRun_ClaudeIsolated(t *testing.T) {
	m := newMachine(t, "claude")
	m.signIn(t, "claude")

	reply, session := m.run(t, "claude", _skillsPrompt)

	if !strings.HasSuffix(reply, _claudeSkills) {
		t.Errorf("reply %q, want it to end with %q", reply, _claudeSkills)
	}
	if session != _claudeSkillsID {
		t.Errorf("session %q, want %q", session, _claudeSkillsID)
	}
	calls := m.sessions(t, "claude")
	if len(calls) != 1 {
		t.Fatalf("claude -p ran %d times, want 1", len(calls))
	}
	m.assertClaudeIsolated(t, calls[0])
	if slices.Contains(calls[0].argv, "--resume") {
		t.Errorf("a new session was started with --resume: %q", calls[0].argv)
	}
}

func TestProviderRun_ClaudeResumes(t *testing.T) {
	m := newMachine(t, "claude")
	m.signIn(t, "claude")

	reply1, session1 := m.run(t, "claude", _factPrompt)
	reply2, session2 := m.run(t, "claude", "--resume", session1, _recallPrompt)

	if reply1 != "ok" || reply2 != "heron" {
		t.Errorf("replies %q, %q; want %q, %q", reply1, reply2, "ok", "heron")
	}
	if session1 != _claudeFactID || session2 != session1 {
		t.Errorf("sessions %q, %q; want %q for both", session1, session2, _claudeFactID)
	}
	calls := m.sessions(t, "claude")
	if len(calls) != 2 {
		t.Fatalf("claude -p ran %d times, want 2", len(calls))
	}
	if got := flagValue(calls[1].argv, "--resume"); got != session1 {
		t.Errorf("second run passed --resume %q, want %q; argv %q", got, session1, calls[1].argv)
	}
	m.assertClaudeIsolated(t, calls[1])
}

func TestProviderRun_CodexIsolated(t *testing.T) {
	m := newMachine(t, "codex")
	m.signIn(t, "codex")

	reply, session := m.run(t, "codex", _skillsPrompt)

	if !strings.HasSuffix(reply, _codexSkills) {
		t.Errorf("reply %q, want it to end with %q", reply, _codexSkills)
	}
	if session != _codexThreadID {
		t.Errorf("session %q, want %q", session, _codexThreadID)
	}
	calls := m.sessions(t, "codex")
	if len(calls) != 1 {
		t.Fatalf("codex app-server ran %d times, want 1", len(calls))
	}
	m.assertCodexIsolated(t, calls[0], "thread/start")
}

func TestProviderRun_CodexResumes(t *testing.T) {
	m := newMachine(t, "codex")
	m.signIn(t, "codex")

	reply1, session1 := m.run(t, "codex", _factPrompt)
	reply2, session2 := m.run(t, "codex", "--resume", session1, _recallPrompt)

	if reply1 != "ok" || reply2 != "heron" {
		t.Errorf("replies %q, %q; want %q, %q", reply1, reply2, "ok", "heron")
	}
	if session1 != _codexThreadID || session2 != session1 {
		t.Errorf("sessions %q, %q; want %q for both", session1, session2, _codexThreadID)
	}
	calls := m.sessions(t, "codex")
	if len(calls) != 2 {
		t.Fatalf("codex app-server ran %d times, want 2", len(calls))
	}
	resume := request(calls[1], "thread/resume")
	if resume == nil {
		t.Fatalf("second run sent no thread/resume; stdin:\n%s", strings.Join(calls[1].stdin, "\n"))
	}
	if resume.ThreadID != session1 {
		t.Errorf("thread/resume threadId %q, want %q", resume.ThreadID, session1)
	}
	m.assertCodexIsolated(t, calls[1], "thread/resume")
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
