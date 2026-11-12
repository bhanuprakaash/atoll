package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: no-repo-shell-contract.md.

const _readmeMarker = "REEF-README-MARKER"

// repoWorld is a machine with Claude, a worktree root and two registered repos.
type repoWorld struct {
	m                       *machine
	s                       *server
	root                    string
	alpha, beta             registeredRepo
	alphaThread, betaThread thread // threads of another Bot on each repo: their clean checkouts are in <data>/threads
}

func newRepoWorld(t *testing.T, m *machine, s *server) *repoWorld {
	t.Helper()
	w := &repoWorld{m: m, s: s, root: t.TempDir()}
	s.do(t, "PUT", "/api/settings", map[string]string{"worktree_root": w.root}, nil, http.StatusNoContent)
	seed := func(name string) registeredRepo {
		dir := t.TempDir()
		gitIn(t, dir, "init", "-b", "main")
		writeFile(t, filepath.Join(dir, "README.md"), _readmeMarker+" "+name+"\nsecond line\n")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-m", "seed")
		var r registeredRepo
		s.do(t, "POST", "/api/repos", map[string]string{"name": name, "path": dir}, &r, http.StatusCreated)
		return r
	}
	w.alpha, w.beta = seed("alpha"), seed("beta")
	reader := w.bot(t, "Reader", "all", "auto")
	w.alphaThread, w.betaThread = w.newThread(t, reader, w.alpha.ID), w.newThread(t, reader, w.beta.ID)
	return w
}

func (w *repoWorld) bot(t *testing.T, name string, access any, mode string) bot {
	t.Helper()
	var b bot
	w.s.do(t, "POST", "/api/bots", map[string]any{"name": name, "persona": "Answers.", "provider": "claude", "repo_access": access, "mode": mode}, &b, http.StatusCreated)
	return b
}

// session runs one message in a new thread of b (on repoID, when set) and returns the thread, the
// finished run and the Claude process that served it.
func (w *repoWorld) session(t *testing.T, b bot, repoID, text string) (thread, runInfo, *call) {
	t.Helper()
	th := w.newThread(t, b, repoID)
	got := w.s.finished(t, w.s.events(t, th), w.s.send(t, th, text).ID)
	sessions := w.m.sessions(t, "claude")
	return th, got, sessions[len(sessions)-1]
}

// newThread starts a thread of b, on repoID when set.
func (w *repoWorld) newThread(t *testing.T, b bot, repoID string) thread {
	t.Helper()
	var th thread
	w.s.do(t, "POST", "/api/bots/"+b.ID+"/threads", map[string]string{"repo_id": repoID}, &th, http.StatusCreated)
	return th
}

// readFiles has the Bot of th read the paths with the fs-read scenario and returns the reply lines.
func (w *repoWorld) readFiles(t *testing.T, th thread, calls ...fsCall) []string {
	t.Helper()
	payload, _ := json.Marshal(calls)
	got := w.s.finished(t, w.s.events(t, th), w.s.send(t, th, "Read files. "+string(payload)).ID)
	if got.Status != "done" {
		t.Fatalf("run: %+v", got)
	}
	lines := strings.Split(got.Reply, "\n")
	if len(lines) != len(calls) {
		t.Fatalf("reply %q, want one line per call (%d)", got.Reply, len(calls))
	}
	return lines
}

type fsCall struct {
	Tool string `json:"tool"`
	Path string `json:"path"`
	Via  string `json:"via,omitempty"`
}

// claudeSandbox is the sandbox and permissions part of a run's --settings.
type claudeSandbox struct {
	Permissions struct {
		Deny []string `json:"deny"`
	} `json:"permissions"`
	Sandbox *struct {
		Enabled                  *bool `json:"enabled"`
		FailIfUnavailable        *bool `json:"failIfUnavailable"`
		AllowUnsandboxedCommands *bool `json:"allowUnsandboxedCommands"`
		AutoAllowBashIfSandboxed *bool `json:"autoAllowBashIfSandboxed"`
		Network                  struct {
			AllowedDomains []string `json:"allowedDomains"`
		} `json:"network"`
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
			DenyRead   []string `json:"denyRead"`
		} `json:"filesystem"`
	} `json:"sandbox"`
}

func sandboxOf(t *testing.T, c *call) claudeSandbox {
	t.Helper()
	var out claudeSandbox
	raw, _ := json.Marshal(claudeSettingsJSON(t, c))
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("--settings: %v", err)
	}
	return out
}

// both is p as Reef knows it and with symlinks resolved, when the two differ. A path that does not
// exist yet is resolved as far as its folders go.
func both(t *testing.T, p string) []string {
	t.Helper()
	return resolvedWith(t, p)
}

func resolvedWith(t *testing.T, p string) []string {
	t.Helper()
	out := []string{p}
	dir, base := p, ""
	for {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			out = append(out, filepath.Join(r, base))
			break
		}
		if dir == filepath.Dir(dir) {
			break
		}
		base = filepath.Join(filepath.Base(dir), base)
		dir = filepath.Dir(dir)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// assertHides checks that the run c has the sandbox on with the settings docs/design.md gives
// (Bots, Repo access on Claude) and hides each of paths: in denyRead, and as Read and Edit deny rules.
func assertHides(t *testing.T, what string, c *call, paths []string) {
	t.Helper()
	got := sandboxOf(t, c)
	sb := got.Sandbox
	if sb == nil {
		t.Errorf("%s: --settings has no sandbox, want it on with denyRead of %q", what, paths)
		return
	}
	for name, v := range map[string]struct {
		got  *bool
		want bool
	}{
		"enabled":                  {sb.Enabled, true},
		"failIfUnavailable":        {sb.FailIfUnavailable, true},
		"allowUnsandboxedCommands": {sb.AllowUnsandboxedCommands, false},
		"autoAllowBashIfSandboxed": {sb.AutoAllowBashIfSandboxed, false},
	} {
		if v.got == nil || *v.got != v.want {
			t.Errorf("%s: sandbox.%s %v, want %v", what, name, v.got, v.want)
		}
	}
	if !slices.Contains(sb.Filesystem.AllowWrite, "/") {
		t.Errorf("%s: sandbox.filesystem.allowWrite %q, want \"/\": the sandbox only hides reads, a Bot writes where it did", what, sb.Filesystem.AllowWrite)
	}
	if !slices.Contains(sb.Network.AllowedDomains, "*") {
		t.Errorf("%s: sandbox.network.allowedDomains %q, want \"*\": a Bot's network is as open as it was", what, sb.Network.AllowedDomains)
	}
	for _, p := range paths {
		if !slices.Contains(sb.Filesystem.DenyRead, p) {
			t.Errorf("%s: sandbox.filesystem.denyRead %q does not hold %s", what, sb.Filesystem.DenyRead, p)
		}
		for _, kind := range []string{"Read", "Edit"} {
			if rule := kind + "(/" + p + "/**)"; !slices.Contains(got.Permissions.Deny, rule) {
				t.Errorf("%s: permissions.deny %q does not hold %s", what, got.Permissions.Deny, rule)
			}
		}
	}
}

// hiddenFor is every path a Bot that lacks the repos hides: each repo's folder, its worktree folder
// and the folders of the threads on it (their read-only checkouts), each as Reef knows it and resolved.
func (w *repoWorld) hiddenFor(t *testing.T, lacked ...registeredRepo) []string {
	t.Helper()
	var paths []string
	for _, r := range lacked {
		th := w.alphaThread
		if r.ID == w.beta.ID {
			th = w.betaThread
		}
		paths = append(paths, both(t, r.Path)...)
		paths = append(paths, both(t, filepath.Join(w.root, r.Name))...)
		paths = append(paths, both(t, filepath.Join(w.m.data, "threads", th.ID))...)
	}
	return paths
}

func TestNoRepoShell_ClaudeHidesReposTheBotLacks(t *testing.T) {
	m, s := start(t, "claude")
	w := newRepoWorld(t, m, s)
	for _, mode := range _modes {
		none := w.bot(t, "None-"+mode, []string{}, mode)
		_, _, c := w.session(t, none, "", _hey)
		assertHides(t, "a Bot with no repo access, "+mode, c, w.hiddenFor(t, w.alpha, w.beta))

		onlyAlpha := w.bot(t, "Alpha-"+mode, []string{w.alpha.ID}, mode)
		_, _, c = w.session(t, onlyAlpha, w.alpha.ID, _hey)
		assertHides(t, "a Bot with access to alpha only, "+mode, c, w.hiddenFor(t, w.beta))
		got := sandboxOf(t, c)
		if got.Sandbox == nil {
			continue
		}
		for _, p := range both(t, w.alpha.Path) {
			if slices.Contains(got.Sandbox.Filesystem.DenyRead, p) || slices.Contains(got.Permissions.Deny, "Read(/"+p+"/**)") {
				t.Errorf("%s: alpha, a repo the Bot may use, is hidden: %s", mode, p)
			}
		}
		for _, p := range slices.Concat(both(t, filepath.Join(w.root, w.alpha.Name)), both(t, filepath.Join(m.data, "threads", w.alphaThread.ID))) {
			if slices.Contains(got.Sandbox.Filesystem.DenyRead, p) {
				t.Errorf("%s: a folder of alpha, a repo the Bot may use, is hidden: %s", mode, p)
			}
		}
	}
}

func TestNoRepoShell_UnchangedWhenNothingIsLacking(t *testing.T) {
	m, s := start(t, "claude")
	w := newRepoWorld(t, m, s)
	all := w.bot(t, "All", "all", "auto")
	onlyBoth := w.bot(t, "Both", []string{w.alpha.ID, w.beta.ID}, "auto")
	for name, b := range map[string]bot{"All": all, "Both": onlyBoth} {
		_, _, c := w.session(t, b, "", _hey)
		if got := sandboxOf(t, c); got.Sandbox != nil || len(got.Permissions.Deny) != 0 {
			t.Errorf("%s: a Bot that lacks no repo has sandbox %+v and deny %q, want neither", name, got.Sandbox, got.Permissions.Deny)
		}
	}

	// Chief and the title runs have no built-in tools, so a sandbox there only adds a start that can fail.
	heySystem(t, s, "claude", chief(t, s).bot)
	calls := m.calls(t, "claude")
	for _, c := range append(slices.Clone(calls), m.titles(t, "claude")...) {
		if toolsFlag(c) == "" {
			if got := sandboxOf(t, c); got.Sandbox != nil || len(got.Permissions.Deny) != 0 {
				t.Errorf("run with --tools \"\": sandbox %+v and deny %q, want neither", got.Sandbox, got.Permissions.Deny)
			}
		}
	}
}

// assertNoRead checks every line of a reply is a refusal with nothing of the file, and that no
// path in made was written.
func assertNoRead(t *testing.T, lines []string, made ...string) {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, _readmeMarker) || !strings.Contains(line, ": refused: ") {
			t.Errorf("call %d, %q: want refused, with nothing of the file", i+1, line)
		}
	}
	for _, p := range made {
		if b, err := os.ReadFile(p); err == nil {
			t.Errorf("%s was created with %q", p, b)
		}
	}
}

func TestNoRepoShell_BotWithoutAccessCannotReadTheRepo(t *testing.T) {
	m, s := start(t, "claude")
	w := newRepoWorld(t, m, s)

	// Another Bot's thread on beta gets its read-only checkout at its first run.
	w.s.finished(t, w.s.events(t, w.betaThread), w.s.send(t, w.betaThread, _hey).ID)
	checkout := filepath.Join(m.data, "threads", w.betaThread.ID, "README.md")
	if b, err := os.ReadFile(checkout); err != nil || !strings.Contains(string(b), _readmeMarker) {
		t.Fatalf("the read-only checkout %s: %q, %v", checkout, b, err)
	}

	readme, written := filepath.Join(w.beta.Path, "README.md"), filepath.Join(w.beta.Path, "written.txt")
	for _, mode := range _modes {
		none := w.bot(t, "None-"+mode, []string{}, mode)
		lines := w.readFiles(t, w.newThread(t, none, ""),
			fsCall{Tool: "Bash", Path: readme},
			fsCall{Tool: "Bash", Path: readme, Via: "python"},
			fsCall{Tool: "Read", Path: readme},
			fsCall{Tool: "Glob", Path: w.beta.Path},
			fsCall{Tool: "Grep", Path: w.beta.Path},
			fsCall{Tool: "Write", Path: written},
			fsCall{Tool: "Bash", Path: checkout, Via: "python"},
			fsCall{Tool: "Read", Path: checkout},
		)
		assertNoRead(t, lines, written)
	}
}

func TestNoRepoShell_BotWithAccessToAnotherRepoCannotReadThisOne(t *testing.T) {
	m, s := start(t, "claude")
	w := newRepoWorld(t, m, s)
	onlyAlpha := w.bot(t, "Alpha", []string{w.alpha.ID}, "auto")
	th := w.newThread(t, onlyAlpha, w.alpha.ID)

	beta, alpha := filepath.Join(w.beta.Path, "README.md"), filepath.Join(w.alpha.Path, "README.md")
	lines := w.readFiles(t, th,
		fsCall{Tool: "Bash", Path: beta},
		fsCall{Tool: "Bash", Path: beta, Via: "python"},
		fsCall{Tool: "Read", Path: beta},
		fsCall{Tool: "Bash", Path: alpha, Via: "python"},
		fsCall{Tool: "Bash", Path: alpha},
		fsCall{Tool: "Read", Path: alpha},
		// The thread's own folder, and another thread's on alpha, are checkouts of alpha in <data>/threads.
		fsCall{Tool: "Bash", Path: filepath.Join(m.data, "threads", th.ID, "README.md"), Via: "python"},
		fsCall{Tool: "Read", Path: filepath.Join(m.data, "threads", th.ID, "README.md")},
	)
	assertNoRead(t, lines[:3])
	for i, line := range lines[3:] {
		if !strings.HasSuffix(line, ": "+_readmeMarker+" alpha") {
			t.Errorf("call %d, %q: want alpha's first line, a repo the Bot may use", i+4, line)
		}
	}
}

func TestNoRepoShell_StillAnswersAndUsesConnectionsAndSkills(t *testing.T) {
	f := newMCPFixture(t, "claude")
	w := newRepoWorld(t, f.m, f.s)

	var skill librarySkill
	f.s.do(t, "POST", "/api/skills", map[string]string{"name": "reef-greeting", "content": fake.GreetingSkill}, &skill, http.StatusCreated)
	var b bot
	f.s.do(t, "POST", "/api/bots", map[string]any{
		"name": "Talker", "persona": "Answers questions.", "provider": "claude", "repo_access": []string{},
		"skills": []string{skill.ID}, "mcp_servers": []string{f.remote.ID},
	}, &b, http.StatusCreated)

	// A recording plays on a new session only, so each ask gets a thread.
	var th thread
	ask := func(text string) runInfo {
		th = f.s.newThread(t, b)
		return f.s.finished(t, f.s.events(t, th), f.s.send(t, th, text).ID)
	}
	if got := ask(_hey); got.Status != "done" || got.Reply != "Hello." {
		t.Fatalf("a question: %+v", got)
	}
	if got := ask(_useSkills); got.Status != "done" || !strings.Contains(got.Reply, "Used reef-greeting") {
		t.Fatalf("its skill: %+v", got)
	}
	th = f.s.newThread(t, b)
	got, _ := f.use(t, th, f.s.events(t, th), true, mcpCall("docs-remote", "answer", `{}`))
	if got.Status != "done" || got.Reply != replyLines("docs-remote/answer: http-answer-42") {
		t.Fatalf("its connection: %+v", got)
	}
	if called, _ := f.http.calls(); called != 1 {
		t.Errorf("the connection's server was called %d times, want 1", called)
	}

	calls := f.m.sessions(t, "claude")
	assertHides(t, "the Bot that answers", calls[len(calls)-1], w.hiddenFor(t, w.alpha, w.beta))
	for _, c := range calls {
		if flagValue(c.argv, "--mcp-config") == "" || flagValue(c.argv, "--allowedTools") == "" {
			t.Errorf("argv %q lost its MCP config or allowed tools", c.argv)
		}
	}
}
