package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// Contract: chief-no-shell-contract.md.

// _rpcCode matches the line the fake adds to a platform call that failed with a JSON-RPC error.
var _rpcCode = regexp.MustCompile(`\n\(JSON-RPC error (-?\d+)\)$`)

const _invalidParams = "-32602"

// chiefTool has Chief call one tool. It returns the result text, or for an error the message and
// the JSON-RPC code ("" for a tool result with isError).
func chiefTool(t *testing.T, s *server, tool string, args map[string]any) (text, code string, failed bool) {
	t.Helper()
	reply := chiefCall(t, s, tool, args).reply
	msg, isErr := strings.CutPrefix(reply, "Error: ")
	if !isErr {
		return reply, "", false
	}
	if m := _rpcCode.FindStringSubmatch(msg); m != nil {
		return strings.TrimSuffix(msg, m[0]), m[1], true
	}
	return msg, "", true
}

func TestChiefNoShell_ClaudeRunsWithoutTools(t *testing.T) {
	m, s := start(t, "claude")
	chiefBot := chief(t, s).bot
	var keeper, plain bot
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Keeper", "persona": "Keeps the reef.", "provider": "claude", "repo_access": "all", "can_manage_platform": true}, &keeper, http.StatusCreated)
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Plain", "persona": "Answers.", "provider": "claude", "repo_access": []string{}}, &plain, http.StatusCreated)
	coder := s.newBot(t, "Coder", "claude")

	lastArgv := func(b bot) []string {
		heySystem(t, s, "claude", b)
		sessions := m.sessions(t, "claude")
		return sessions[len(sessions)-1].argv
	}
	// tools is the value of --tools and whether the flag is there.
	tools := func(argv []string) (string, bool) {
		i := slices.Index(argv, "--tools")
		if i < 0 || i+1 >= len(argv) {
			return "", false
		}
		return argv[i+1], true
	}

	for _, mode := range _modes {
		s.do(t, "PUT", "/api/bots/chief", map[string]string{"mode": mode}, nil, http.StatusOK)
		argv := lastArgv(chiefBot)
		if v, ok := tools(argv); !ok || v != "" {
			t.Errorf("Chief on %s: --tools present %v with value %q, want present and empty; argv %q", mode, ok, v, argv)
		}
		if !strings.Contains(flagValue(argv, "--mcp-config"), `"atoll"`) || flagValue(argv, "--allowedTools") != "mcp__atoll" {
			t.Errorf("Chief on %s: want the atoll MCP config and --allowedTools mcp__atoll kept; argv %q", mode, argv)
		}
	}

	for _, b := range []bot{keeper, plain, coder} {
		if v, ok := tools(lastArgv(b)); ok {
			t.Errorf("%s got --tools %q; only Chief runs without tools", b.Name, v)
		}
	}
}

func TestChiefNoShell_CodexChiefStillRuns(t *testing.T) {
	_, s := start(t, "codex")
	b := chief(t, s)
	if b.Provider != "codex" {
		t.Fatalf("Chief on a Codex-only machine runs on %q, want codex", b.Provider)
	}
	platformBlock(t, heySystem(t, s, "codex", b.bot))
}

func TestChiefNoShell_ImportAgent(t *testing.T) {
	m, s := start(t, "claude")
	agents := filepath.Join(m.home, ".claude", "agents")
	file := filepath.Join(agents, "lagoon.md")
	const body = "---\nname: lagoon\n---\nYou greet visitors.\n"
	writeFile(t, file, body)

	for _, path := range []string{"~/.claude/agents/lagoon.md", file} {
		text, code, failed := chiefTool(t, s, "import_agent", map[string]any{"path": path})
		var got struct{ Name, Path, Content string }
		if failed || json.Unmarshal([]byte(text), &got) != nil || got.Name != "lagoon" || got.Content != body || resolved(t, got.Path)[0] != resolved(t, file)[0] {
			t.Fatalf("import_agent %s: %q (error %v %s), want name lagoon, path %s, content %q", path, text, failed, code, resolved(t, file)[0], body)
		}
	}

	secret := filepath.Join(m.home, ".claude", "secret.md")
	writeFile(t, secret, "outside")
	writeFile(t, filepath.Join(agents, "sub", "deep.md"), "nested")
	writeFile(t, filepath.Join(agents, "notes.txt"), "text")
	writeFile(t, filepath.Join(agents, "big.md"), strings.Repeat("a", 256*1024+1))
	writeFile(t, filepath.Join(agents, "bad.md"), "\xff\xfe not utf-8")
	if err := os.Mkdir(filepath.Join(agents, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(agents, "pipe.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	writeFile(t, outside, "elsewhere")
	if err := os.Symlink(outside, filepath.Join(agents, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(agents, "up.md")); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct{ name, path, want string }{
		{"dot-dot out of the folder", "~/.claude/agents/../secret.md", "agents"},
		{"dot-dot in an absolute path", agents + "/../secret.md", "agents"},
		{"nested folder", "~/.claude/agents/sub/deep.md", "agents"},
		{"not markdown", "~/.claude/agents/notes.txt", ".md"},
		{"symlink to a file outside", "~/.claude/agents/link.md", "agents"},
		{"symlink to a file in .claude", "~/.claude/agents/up.md", "agents"},
		{"directory", "~/.claude/agents/dir.md", ".md"},
		{"fifo", "~/.claude/agents/pipe.md", ".md"},
		{"over 256 KiB", "~/.claude/agents/big.md", "256"},
		{"not UTF-8", "~/.claude/agents/bad.md", "UTF-8"},
		{"missing", "~/.claude/agents/gone.md", "not found"},
		{"a folder", t.TempDir(), "agents"},
	} {
		text, code, failed := chiefTool(t, s, "import_agent", map[string]any{"path": tt.path})
		if !failed {
			t.Errorf("%s: import_agent %s answered %q, want an error", tt.name, tt.path, text)
			continue
		}
		if code != _invalidParams || !strings.Contains(text, tt.want) {
			t.Errorf("%s: error %q with code %q, want code %s and %q in the message", tt.name, text, code, _invalidParams, tt.want)
		}
	}
}

func TestChiefNoShell_ImportAgentThroughSymlinkedFolder(t *testing.T) {
	m, s := start(t, "claude")
	dotfiles := filepath.Join(t.TempDir(), "dotfiles-agents")
	writeFile(t, filepath.Join(dotfiles, "lagoon.md"), "---\nname: lagoon\n---\nHi.\n")
	if err := os.MkdirAll(filepath.Join(m.home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, filepath.Join(m.home, ".claude", "agents")); err != nil {
		t.Fatal(err)
	}
	text, _, failed := chiefTool(t, s, "import_agent", map[string]any{"path": "~/.claude/agents/lagoon.md"})
	var got struct{ Name, Path string }
	if failed || json.Unmarshal([]byte(text), &got) != nil || got.Name != "lagoon" || resolved(t, got.Path)[0] != resolved(t, filepath.Join(dotfiles, "lagoon.md"))[0] {
		t.Fatalf("import_agent through a symlinked agents folder: %q (error %v)", text, failed)
	}
}

func TestChiefNoShell_ImportAgentOnlyForPlatformBots(t *testing.T) {
	m, s := start(t, "claude")
	writeFile(t, filepath.Join(m.home, ".claude", "agents", "lagoon.md"), "---\nname: lagoon\n---\nHi.\n")
	if _, _, failed := chiefTool(t, s, "import_agent", map[string]any{"path": "~/.claude/agents/lagoon.md"}); failed {
		t.Fatal("Chief's import_agent call failed")
	}
	// The fake fails the run when the Bot lists import_agent or its guessed call works.
	th := s.newThread(t, s.newBot(t, "Plain", "claude"))
	got := s.finished(t, s.events(t, th), s.send(t, th, "Import the skill folder "+filepath.Join(m.home, ".claude", "skills", "x")).ID)
	if got.Status != "done" || got.Reply != "import_skill denied." {
		t.Fatalf("Bot without the platform switch: %+v", got)
	}
}

func TestChiefNoShell_ImportSkillOnlyFromSkillFolders(t *testing.T) {
	m, s := start(t, "claude")
	skill := func(dir, name string) string {
		path := filepath.Join(dir, name)
		writeFile(t, filepath.Join(path, "SKILL.md"), "---\nname: "+name+"\ndescription: Greets.\n---\nHello.\n")
		return path
	}
	repoPath := t.TempDir()
	gitIn(t, repoPath, "init", "-b", "main")
	var repo registeredRepo
	s.do(t, "POST", "/api/repos", map[string]string{"name": "seeded", "path": repoPath}, &repo, http.StatusCreated)
	skills := filepath.Join(m.home, ".claude", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct{ name, path string }{
		{"a registered repo's skill folder", skill(filepath.Join(repoPath, ".claude", "skills"), "in-repo")},
		{"a folder in the repo's .agents/skills", skill(filepath.Join(repoPath, ".agents", "skills"), "agents-skill")},
		{"a folder in the user's .agents/skills", skill(filepath.Join(m.home, ".agents", "skills"), "home-agents")},
		{"a folder in /tmp", skill(t.TempDir(), "tmp-skill")},
		{"the repo itself", repoPath},
		{"the skills folder itself", skills},
		{"a folder two levels down", skill(filepath.Join(skills, "group"), "nested")},
		{"a dot-dot way out", filepath.Join(skills, "..", "elsewhere", filepath.Base(skill(filepath.Join(m.home, ".claude", "elsewhere"), "up")))},
	} {
		text, code, failed := chiefTool(t, s, "import_skill", map[string]any{"path": tt.path})
		if !failed {
			t.Errorf("%s: import_skill %s answered %q, want an error", tt.name, tt.path, text)
			continue
		}
		if code != _invalidParams || !strings.Contains(text, ".claude/skills") || !strings.Contains(text, ".codex/skills") {
			t.Errorf("%s: error %q with code %q, want code %s naming ~/.claude/skills and ~/.codex/skills", tt.name, text, code, _invalidParams)
		}
	}
	var library []librarySkill
	s.do(t, "GET", "/api/skills", nil, &library, http.StatusOK)
	if len(library) != 0 {
		t.Errorf("refused imports left skills in the library: %+v", library)
	}

	for _, root := range []string{".claude", ".codex"} {
		path := skill(filepath.Join(m.home, root, "skills"), "greet"+root)
		if text, _, failed := chiefTool(t, s, "import_skill", map[string]any{"path": path}); failed || !strings.Contains(text, `"created"`) {
			t.Errorf("import_skill %s: %q (error %v), want created", path, text, failed)
		}
	}
	s.do(t, "GET", "/api/skills", nil, &library, http.StatusOK)
	if len(library) != 2 {
		t.Errorf("library after the two allowed imports: %+v", library)
	}
}

func TestChiefNoShell_PromptNamesImportAgent(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			block := platformBlock(t, heySystem(t, s, p, chief(t, s).bot))
			if rule := strings.Join(section(t, block, _importHeading), "\n"); !strings.Contains(rule, "import_agent") {
				t.Errorf("%q does not name import_agent:\n%s", _importHeading, rule)
			}
			for _, bad := range []string{"read it,", "file tool", "Read tool"} {
				if strings.Contains(block, bad) {
					t.Errorf("platform block still tells Chief to read files itself (%q):\n%s", bad, block)
				}
			}
			if n := len(block); n > _platformBlockMaxBytes {
				t.Errorf("platform block is %d bytes, want at most %d", n, _platformBlockMaxBytes)
			}
		})
	}
}

func TestChiefNoShell_ChiefStillDoesItsJob(t *testing.T) {
	m, s := start(t, "claude")
	repoPath := t.TempDir()
	gitIn(t, repoPath, "init", "-b", "main")
	local := filepath.Join(m.home, ".claude")
	const agent = "---\nname: lagoon\ndescription: Greets visitors.\n---\nYou greet visitors to the lagoon.\n"
	writeFile(t, filepath.Join(local, "agents", "lagoon.md"), agent)
	writeFile(t, filepath.Join(local, "skills", "reef-greeting", "SKILL.md"), "---\nname: reef-greeting\ndescription: Give the Reef greeting.\n---\nHello from the lagoon.\n")

	reg := chiefCall(t, s, "register_repo", map[string]any{"name": "seeded", "path": repoPath})
	var repo registeredRepo
	if err := json.Unmarshal([]byte(reg.reply), &repo); err != nil || repo.ID == "" {
		t.Fatalf("register_repo reply %q", reg.reply)
	}

	imp := chiefCall(t, s, "import_agent", map[string]any{"path": "~/.claude/agents/lagoon.md"})
	var got struct{ Name, Content string }
	if err := json.Unmarshal([]byte(imp.reply), &got); err != nil || got.Name != "lagoon" || got.Content != agent {
		t.Fatalf("import_agent reply %q, want lagoon and its file", imp.reply)
	}

	sk := chiefCall(t, s, "import_skill", map[string]any{"path": "~/.claude/skills/reef-greeting"})
	var imported importResult
	if err := json.Unmarshal([]byte(sk.reply), &imported); err != nil || imported.Status != "created" || imported.Skill == nil {
		t.Fatalf("import_skill reply %q, want created", sk.reply)
	}

	created := chiefCall(t, s, "create_bot", map[string]any{"name": "lagoon", "persona": "You greet visitors to the lagoon.", "provider": "claude", "skills": []string{imported.Skill.ID}})
	id := createdID(t, created)
	if b := s.bot(t, id); b.Name != "lagoon" {
		t.Fatalf("created Bot: %+v", b)
	}
	platformChange(t, created.events, "chief", created.thread, created.run, "bot", id, "create")
}
