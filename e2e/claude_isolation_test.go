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
)

// Contract: claude-isolation-contract.md.

// _shellPrefixScript is the script docs/design.md → Providers gives Bash through
// CLAUDE_CODE_SHELL_PREFIX.
const _shellPrefixScript = `#!/bin/bash
for v in $(compgen -e | grep -E '^(CLAUDE|ATOLL_MCP_)'); do unset "$v"; done
exec bash -c "$*"`

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
	if got, want := disallowedTools(c), []string{"Task", "Workflow"}; !slices.Equal(got, want) {
		t.Errorf("%s: --disallowedTools %q, want %q", what, got, want)
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
