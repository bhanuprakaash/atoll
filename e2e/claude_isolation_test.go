package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Contract: claude-isolation-contract.md.

// _shellPrefixScript is the script docs/design.md → Providers gives Bash through
// CLAUDE_CODE_SHELL_PREFIX.
const _shellPrefixScript = `#!/bin/bash
for v in $(compgen -e | grep -E '^(CLAUDE|ATOLL_MCP_)'); do unset "$v"; done
exec bash -c "$*"`

// _botTools is the whole set of built-in tools a Claude Bot has: the value of --tools.
const _botTools = "Bash,Edit,Glob,Grep,NotebookEdit,Read,Skill,WebFetch,WebSearch,Write"

// _globReplacer replaces the characters that are glob syntax by ? in the folder names of a claudeMdExcludes pattern.
var _globReplacer = strings.NewReplacer(`\`, "?", "*", "?", "?", "?", "[", "?", "]", "?", "{", "?", "}", "?", "(", "?", ")", "?")

// ancestorExcludes is the claudeMdExcludes docs/design.md gives a run in cwd: two patterns for every
// folder above cwd, as given and with symlinks resolved, sorted.
func ancestorExcludes(t *testing.T, cwd string) []string {
	t.Helper()
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range []string{cwd, resolvedCwd} {
		for d := filepath.Dir(p); ; d = filepath.Dir(d) {
			base := strings.TrimSuffix(_globReplacer.Replace(d), "/")
			out = append(out, base+"/*.md", base+"/.claude/**")
			if d == "/" {
				break
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// claudeSettingsJSON is the JSON object of every --settings flag of c, merged by key.
func claudeSettingsJSON(t *testing.T, c *call) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for i, a := range c.argv {
		if a != "--settings" || i+1 >= len(c.argv) {
			continue
		}
		b := []byte(c.argv[i+1])
		if !strings.HasPrefix(strings.TrimSpace(c.argv[i+1]), "{") {
			var err error
			if b, err = os.ReadFile(c.argv[i+1]); err != nil {
				t.Fatal(err)
			}
		}
		var one map[string]json.RawMessage
		if err := json.Unmarshal(b, &one); err != nil {
			t.Fatalf("--settings %q is not a JSON object: %v", c.argv[i+1], err)
		}
		for k, v := range one {
			out[k] = v
		}
	}
	return out
}

// toolsFlag is the value of --tools, or "<none>" when the flag is not there.
func toolsFlag(c *call) string {
	i := slices.Index(c.argv, "--tools")
	if i < 0 || i+1 >= len(c.argv) {
		return "<none>"
	}
	return c.argv[i+1]
}

// disallowedTools is the set of tools c's --disallowedTools flags name, space or comma separated.
func disallowedTools(c *call) []string {
	var out []string
	for i, a := range c.argv {
		if a != "--disallowedTools" && a != "--disallowed-tools" {
			continue
		}
		for _, v := range c.argv[i+1:] {
			if strings.HasPrefix(v, "--") {
				break
			}
			out = append(out, strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' })...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// assertOwnerSetupOff checks the flags and env that keep the owner's own Claude setup out of the
// run c. The fake sees its cwd with symlinks resolved, so the cwd as Reef gave it is that with a
// resolved folder of bases (a folder as Reef knows it) put back as given.
func (m *machine) assertOwnerSetupOff(t *testing.T, c *call, what string, bases ...string) {
	t.Helper()
	if got := flagValue(c.argv, "--setting-sources"); got != "project" {
		t.Errorf("%s: --setting-sources %q, want project", what, got)
	}
	if got := toolsFlag(c); got != _botTools {
		t.Errorf("%s: --tools %q, want %q", what, got, _botTools)
	}
	for _, tool := range disallowedTools(c) {
		if tool == "Task" || tool == "Workflow" {
			t.Errorf("%s: --disallowedTools names %s, want the --tools allowlist instead", what, tool)
		}
	}

	settings := claudeSettingsJSON(t, c)
	if got := string(settings["autoMemoryEnabled"]); got != "false" {
		t.Errorf("%s: --settings autoMemoryEnabled %q, want false", what, got)
	}
	cwd := c.cwd
	for _, b := range bases {
		if rest, ok := strings.CutPrefix(cwd, resolved(t, b)[0]); ok && (rest == "" || rest[0] == '/') {
			cwd = b + rest
			break
		}
	}
	if !filepath.IsAbs(cwd) {
		t.Fatalf("%s: cwd %q is not absolute", what, cwd)
	}
	var excludes []string
	if err := json.Unmarshal(settings["claudeMdExcludes"], &excludes); err != nil {
		t.Errorf("%s: --settings claudeMdExcludes %s is not a list of strings", what, settings["claudeMdExcludes"])
	}
	got := slices.Clone(excludes)
	slices.Sort(got)
	if dup := slices.Compact(slices.Clone(got)); len(dup) != len(got) {
		t.Errorf("%s: claudeMdExcludes has duplicates: %q", what, excludes)
	}
	if want := ancestorExcludes(t, cwd); !slices.Equal(slices.Compact(got), want) {
		t.Errorf("%s: claudeMdExcludes\n%q\nwant every folder above %s, as given and resolved:\n%q", what, got, cwd, want)
	}
	own := _globReplacer.Replace(cwd)
	for _, e := range excludes {
		if strings.HasPrefix(e, own+"/") {
			t.Errorf("%s: claudeMdExcludes lists %q, which hides the repo's own files in %s", what, e, cwd)
		}
	}

	prefix := c.env["CLAUDE_CODE_SHELL_PREFIX"]
	want := filepath.Join(m.data, "bin", "claude-shell-prefix")
	if !filepath.IsAbs(prefix) || strings.ContainsAny(prefix, `"'`) || resolved(t, filepath.Dir(prefix))[0] != resolved(t, filepath.Dir(want))[0] || filepath.Base(prefix) != "claude-shell-prefix" {
		t.Errorf("%s: CLAUDE_CODE_SHELL_PREFIX=%q, want the plain absolute path %s", what, prefix, want)
	}

	var memory []string
	_ = filepath.WalkDir(filepath.Join(m.providerDir("claude"), "projects"), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "memory" {
			memory = append(memory, p)
		}
		return nil
	})
	if len(memory) != 0 {
		t.Errorf("%s: claude made memory folders %q, want none", what, memory)
	}
}

// ownerSetupMachine is a machine with a signed-in Claude whose data dir has a space, as on macOS.
func ownerSetupMachine(t *testing.T) *machine {
	t.Helper()
	m := newMachine(t, "claude")
	m.data = filepath.Join(t.TempDir(), "Application Support", "Atoll")
	if err := os.MkdirAll(m.data, 0o755); err != nil {
		t.Fatal(err)
	}
	m.signIn(t, "claude")
	return m
}

func TestClaudeIsolation_EveryModeFreshAndResumed(t *testing.T) {
	t.Parallel()
	m := ownerSetupMachine(t)
	s := m.serve(t)
	for _, mode := range _modes {
		var b bot
		s.do(t, "POST", "/api/bots", map[string]string{"name": "Mode " + mode, "persona": "x", "provider": "claude", "mode": mode}, &b, http.StatusCreated)
		th := s.newThread(t, b)
		ch := s.events(t, th)
		s.finished(t, ch, s.send(t, th, _keepPrompt).ID)
		s.finished(t, ch, s.send(t, th, _askPrompt).ID)
	}
	sessions := m.sessions(t, "claude")
	if len(sessions) != 2*len(_modes) {
		t.Fatalf("claude ran %d times, want %d", len(sessions), 2*len(_modes))
	}
	for i, c := range sessions {
		resumed := i%2 == 1
		if resumed != (flagValue(c.argv, "--resume") != "") {
			t.Fatalf("run %d: --resume %q, want it only on the second run of a thread", i, flagValue(c.argv, "--resume"))
		}
		m.assertOwnerSetupOff(t, c, "mode "+_modes[i/2]+map[bool]string{false: " fresh", true: " resumed"}[resumed], m.data)
	}
}

func TestClaudeIsolation_RepoWorktreeWithGlobAndSpace(t *testing.T) {
	t.Parallel()
	f := newRepoFixture(t, "claude")
	connection, _ := f.connect(t)
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	f.root = filepath.Join(base, "link", `wt g[1]x*{a,b} p(q\d?`)
	f.s.do(t, "PUT", "/api/settings", map[string]string{"worktree_root": f.root}, nil, http.StatusNoContent)

	th, path, ch, _ := f.work(t, f.bot(t, "claude", "all", []string{connection}), _fixRepoPrompt, "fix/seeded-bug")
	f.s.finished(t, ch, f.s.send(t, th, _resumeRepoPrompt).ID)

	sessions := f.m.sessions(t, "claude")
	if len(sessions) != 2 {
		t.Fatalf("claude ran %d times, want 2", len(sessions))
	}
	if !strings.HasPrefix(path, filepath.Join(base, "link")+"/") {
		t.Fatalf("worktree %q is not under the symlinked %s: the test cannot tell the link from the real path", path, filepath.Join(base, "link"))
	}
	if got := sessions[1].cwd; got != resolved(t, path)[0] {
		t.Fatalf("the resumed run started in %q, want the worktree %q", got, resolved(t, path)[0])
	}
	for i, c := range sessions {
		f.m.assertOwnerSetupOff(t, c, []string{"checkout run", "worktree run"}[i], f.root, f.m.data)
	}
	// Both forms of the symlinked ancestor are listed, not only the one claude was started with.
	var excludes []string
	_ = json.Unmarshal(claudeSettingsJSON(t, sessions[1])["claudeMdExcludes"], &excludes)
	for _, dir := range []string{filepath.Join(base, "link"), filepath.Join(resolved(t, base)[0], "real")} {
		if !slices.Contains(excludes, dir+"/*.md") {
			t.Errorf("claudeMdExcludes lacks %s/*.md: %q", dir, excludes)
		}
	}
}

func TestClaudeIsolation_ShellPrefixScript(t *testing.T) {
	t.Parallel()
	m := ownerSetupMachine(t)
	s := m.serve(t)
	prefix := filepath.Join(m.data, "bin", "claude-shell-prefix")

	check := func(when string) os.FileInfo {
		t.Helper()
		fi, err := os.Lstat(prefix)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if fi.Mode() != 0o500 {
			t.Errorf("%s: %s has mode %v, want -r-x------", when, prefix, fi.Mode())
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
			t.Errorf("%s: %s is owned by uid %v, want %d", when, prefix, fi.Sys(), os.Getuid())
		}
		if b, _ := os.ReadFile(prefix); strings.TrimRight(string(b), "\n") != _shellPrefixScript {
			t.Errorf("%s: %s holds\n%s\nwant\n%s", when, prefix, b, _shellPrefixScript)
		}
		if _, err := os.Lstat(filepath.Join(m.data, "bin", ".claude-shell-prefix.tmp")); err == nil {
			t.Errorf("%s: the temporary .claude-shell-prefix.tmp was left behind", when)
		}
		return fi
	}
	first := check("after the first start")

	// A restart writes it again, through a rename, over whatever is there.
	s.kill()
	if err := os.Chmod(prefix, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, prefix, "#!/bin/sh\necho stale\n")
	s = m.serve(t)
	if second := check("after a restart"); os.SameFile(first, second) {
		t.Errorf("a restart rewrote %s in place, want a new file renamed over it", prefix)
	}

	var b bot
	s.do(t, "POST", "/api/bots", map[string]string{"name": "Shell", "persona": "x", "provider": "claude"}, &b, http.StatusCreated)
	th := s.newThread(t, b)
	s.finished(t, s.events(t, th), s.send(t, th, _keepPrompt).ID)
	if got := m.sessions(t, "claude")[0].env["CLAUDE_CODE_SHELL_PREFIX"]; resolved(t, got)[0] != resolved(t, prefix)[0] {
		t.Errorf("CLAUDE_CODE_SHELL_PREFIX=%q, want %s", got, prefix)
	}

	// The script is the one that runs: the secrets go, a normal variable stays, and nested quotes work.
	cmd := exec.CommandContext(t.Context(), prefix, `env | grep -c -E '^(CLAUDE|ATOLL_MCP)'; echo "$KEEP_ME" | tr a-z A-Z; echo "$(echo 'a b')"`)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + m.home, "CLAUDE_FOO=1", "CLAUDE_CONFIG_DIR=/x", "ATOLL_MCP_0=secret", "ATOLL_MCP_12=secret", "KEEP_ME=survives"}
	out, err := cmd.Output()
	if got, want := string(out), "0\nSURVIVES\na b\n"; got != want {
		t.Errorf("the script printed %q (error %v), want %q", got, err, want)
	}
}

// readScript is the script at path without its last newline.
func readScript(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(b), "\n")
}

func TestClaudeIsolation_ShellPrefixSurvivesStaleTemp(t *testing.T) {
	t.Parallel()
	m := ownerSetupMachine(t)
	bin := filepath.Join(m.data, "bin")
	stale := filepath.Join(bin, ".claude-shell-prefix.tmp")
	writeFile(t, stale, "#!/bin/bash\nfor v in $(compg")
	if err := os.Chmod(stale, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stale, 0o600) })

	m.serve(t)

	prefix := filepath.Join(bin, "claude-shell-prefix")
	if got := readScript(t, prefix); got != _shellPrefixScript {
		t.Errorf("%s holds\n%s\nwant\n%s", prefix, got, _shellPrefixScript)
	}
	if fi, err := os.Stat(prefix); err != nil || fi.Mode() != 0o500 {
		t.Errorf("%s: mode %v, error %v, want -r-x------", prefix, fi, err)
	}
	if _, err := os.Lstat(stale); err == nil {
		t.Errorf("the stale %s is still there, want it replaced and renamed away", stale)
	}
}

func TestClaudeIsolation_ShellPrefixReplacedWholeOnRestart(t *testing.T) {
	t.Parallel()
	m := ownerSetupMachine(t)
	prefix := filepath.Join(m.data, "bin", "claude-shell-prefix")
	const old = "#!/bin/bash\n# an older script\nexec bash -c \"$*\""
	writeFile(t, prefix, old+"\n")
	if err := os.Chmod(prefix, 0o500); err != nil {
		t.Fatal(err)
	}

	// A reader opens the path all through the start. A write in place is seen empty or cut short.
	stop, done := make(chan struct{}), make(chan int)
	go func() {
		reads := 0
		defer func() { done <- reads }()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(prefix)
			if got := strings.TrimRight(string(b), "\n"); err != nil || got != old && got != _shellPrefixScript {
				t.Errorf("a read of %s during the start gave %q (error %v), want the old script or the new one whole", prefix, b, err)
				return
			}
			reads++
		}
	}()
	m.serve(t)
	time.Sleep(50 * time.Millisecond)
	close(stop)
	if reads := <-done; reads == 0 {
		t.Error("the reader never read the script")
	}
	if got := readScript(t, prefix); got != _shellPrefixScript {
		t.Errorf("after the start %s holds\n%s\nwant\n%s", prefix, got, _shellPrefixScript)
	}
}

func TestClaudeIsolation_ShellPrefixIsAbsoluteForRelativeDataDir(t *testing.T) {
	t.Parallel()
	m := ownerSetupMachine(t)
	// The server starts in a new temp folder next to the data dir's parent, so this reaches m.data.
	rel := filepath.Join("..", filepath.Base(filepath.Dir(filepath.Dir(m.data))), "Application Support", "Atoll")
	m.env = append(m.env, "ATOLL_DATA_DIR="+rel)
	s := m.serve(t)

	var b bot
	s.do(t, "POST", "/api/bots", map[string]string{"name": "Relative", "persona": "x", "provider": "claude"}, &b, http.StatusCreated)
	th := s.newThread(t, b)
	s.finished(t, s.events(t, th), s.send(t, th, _keepPrompt).ID)

	got := m.sessions(t, "claude")[0].env["CLAUDE_CODE_SHELL_PREFIX"]
	want := filepath.Join(m.data, "bin", "claude-shell-prefix")
	if !filepath.IsAbs(got) || resolved(t, got)[0] != resolved(t, want)[0] {
		t.Errorf("with ATOLL_DATA_DIR=%s, CLAUDE_CODE_SHELL_PREFIX=%q, want the absolute path %s", rel, got, want)
	}
}

func TestClaudeIsolation_ToolsByKindOfRun(t *testing.T) {
	t.Parallel()
	m := ownerSetupMachine(t)
	s := m.serve(t)
	var keeper bot
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Keeper", "persona": "Keeps the reef.", "provider": "claude", "repo_access": "all", "can_manage_platform": true}, &keeper, http.StatusCreated)
	chiefBot := chief(t, s).bot

	tools := func(b bot) *call {
		heySystem(t, s, "claude", b)
		sessions := m.sessions(t, "claude")
		return sessions[len(sessions)-1]
	}
	c := tools(chiefBot)
	_, hasAtoll := mcpServersOf(t, "claude", c)["atoll"]
	if toolsFlag(c) != "" || !hasAtoll || flagValue(c.argv, "--allowedTools") != "mcp__atoll" {
		t.Errorf("Chief: --tools %q, want the empty value, with the atoll MCP config and --allowedTools mcp__atoll kept", toolsFlag(c))
	}
	for _, b := range []bot{keeper, s.newBot(t, "Plain", "claude")} {
		if c := tools(b); toolsFlag(c) != _botTools {
			t.Errorf("%s: --tools %q, want %q", b.Name, toolsFlag(c), _botTools)
		}
	}

	// Title runs get no built-in tools either.
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	s.finished(t, s.events(t, th), s.send(t, th, _titlePrompt).ID)
	titled(t, s, th.ID, _titleWant)
	// Every thread gets its own title run, so no count is pinned.
	titles := m.titles(t, "claude")
	if len(titles) == 0 {
		t.Fatal("no title run started")
	}
	for _, c := range titles {
		if got := toolsFlag(c); got != "" {
			t.Errorf("title run %d --tools %q, want the empty value", c.pid, got)
		}
	}
}

func TestClaudeIsolation_ModelDiscoveryHasTheAllowlist(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	var providers []map[string]any
	s.do(t, "POST", "/api/providers/refresh", nil, &providers, http.StatusOK)

	// The model-list probe starts claude -p, asks for the models and sends no user turn.
	probes := 0
	for _, c := range m.calls(t, "claude") {
		if !slices.Contains(c.argv, "-p") || slices.ContainsFunc(c.stdin, isTurn) || !slices.ContainsFunc(c.stdin, func(l string) bool { return strings.Contains(l, `"initialize"`) }) {
			continue
		}
		probes++
		if got := toolsFlag(c); got != _botTools {
			t.Errorf("model discovery run %d: --tools %q, want %q", c.pid, got, _botTools)
		}
		if got := flagValue(c.argv, "--setting-sources"); got != "project" {
			t.Errorf("model discovery run %d: --setting-sources %q, want project", c.pid, got)
		}
	}
	if probes == 0 {
		t.Fatal("no model discovery run of claude")
	}
}
