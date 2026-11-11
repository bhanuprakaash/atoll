package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Contract: skill-home-paths-contract.md.

// spacedHome gives the machine a HOME with a space in its name, symlinks resolved (on macOS the
// temporary folder is behind /var -> /private/var), and returns it.
func spacedHome(t *testing.T, m *machine) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(m.home)
	if err != nil {
		t.Fatal(err)
	}
	m.home = filepath.Join(base, "Home Dir")
	if err := os.MkdirAll(m.home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _keychains != "" {
		if err := os.MkdirAll(filepath.Join(m.home, "Library"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(_keychains, filepath.Join(m.home, "Library", "Keychains")); err != nil {
			t.Fatal(err)
		}
	}
	return m.home
}

// startResolved starts a machine whose HOME is already resolved, so the server's own home is the
// same text with or without symlink resolution (on macOS the temporary folder is behind /var).
func startResolved(t *testing.T, p string) (*machine, *server) {
	t.Helper()
	m := newMachine(t, p)
	m.home = resolvedHome(t, m)
	m.signIn(t, p)
	return m, m.serve(t)
}

func resolvedHome(t *testing.T, m *machine) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(m.home)
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func skillMD(name, body string) string {
	return "---\nname: " + name + "\ndescription: Test skill.\n---\n" + body
}

// ownRefs is a poteto-mode folder whose files point at their own folder in every form.
func ownRefs(root, dot string) []fixtureFile {
	folder := "/" + dot + "/skills/poteto-mode"
	return []fixtureFile{
		{path: "SKILL.md", content: skillMD("poteto-mode", "Tilde: `~"+folder+"/scripts/run.sh`\n"+
			"Home: $HOME"+folder+"/scripts/run.sh\n"+
			"Braces: ${HOME}"+folder+"/scripts/run.sh\n"+
			"Absolute: "+root+"/scripts/run.sh\n"+
			"Folder: (~"+folder+")\n")},
		{path: "scripts/check", content: _checkScript, exec: true},
		{path: "scripts/run.sh", content: "#!/bin/sh\nexec ~" + folder + "/scripts/helper.sh \"$@\"\n", exec: true},
		{path: "scripts/helper.sh", content: "#!/bin/sh\nexec \"" + root + "/scripts/check\"\n", exec: true},
		{path: "notes.md", content: "See ${HOME}" + folder + "/notes.md and {{ATOLL_SKILL_DIR}}/literal.md\n"},
	}
}

const (
	_ownSkillMD = "---\nname: poteto-mode\ndescription: Test skill.\n---\n" +
		"Tilde: `{{ATOLL_SKILL_DIR}}/scripts/run.sh`\n" +
		"Home: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n" +
		"Braces: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n" +
		"Absolute: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n" +
		"Folder: ({{ATOLL_SKILL_DIR}})\n"
	_ownRun    = "#!/bin/sh\nexec {{ATOLL_SKILL_DIR}}/scripts/helper.sh \"$@\"\n"
	_ownHelper = "#!/bin/sh\nexec \"{{ATOLL_SKILL_DIR}}/scripts/check\"\n"
	_ownNotes  = "See {{ATOLL_SKILL_DIR}}/notes.md and {{ATOLL_SKILL_DIR}}/literal.md\n"
)

// importOwnRefs imports ownRefs from a HOME with a space and returns what the library holds.
func importOwnRefs(t *testing.T, p, dot string) (*machine, *server, string, fileSkill, importResult) {
	t.Helper()
	m := newMachine(t, p)
	home := spacedHome(t, m)
	m.signIn(t, p)
	s := m.serve(t)
	root := filepath.Join(home, dot, "skills", "poteto-mode")
	writeFixture(t, root, ownRefs(root, dot))
	before := treeSum(t, root)
	imp := newImporter(t, s).say(t, "Import the skill folder "+root)
	if imp.res.Status != "created" {
		t.Fatalf("import: %q %+v", imp.summary, imp.res)
	}
	if after := treeSum(t, root); after != before {
		t.Fatal("import changed the source folder")
	}
	return m, s, root, onlySkill(t, s), imp.res
}

func TestSkillHome_OwnFolderBecomesToken(t *testing.T) {
	for _, p := range _providers {
		for _, dot := range []string{".claude", ".codex"} {
			t.Run(p+dot, func(t *testing.T) {
				m, s, root, skill, res := importOwnRefs(t, p, dot)
				wantFiles := []skillFile{
					{Path: "notes.md", Content: _ownNotes},
					{Path: "scripts/check", Content: _checkScript, Executable: true},
					{Path: "scripts/helper.sh", Content: _ownHelper, Executable: true},
					{Path: "scripts/run.sh", Content: _ownRun, Executable: true},
				}
				if skill.Content != _ownSkillMD || !reflect.DeepEqual(skill.Files, wantFiles) {
					t.Fatalf("stored SKILL.md %q files %+v, want the folder as {{ATOLL_SKILL_DIR}}:\n%q\n%+v", skill.Content, skill.Files, _ownSkillMD, wantFiles)
				}
				for _, f := range []string{"SKILL.md", "notes.md", "scripts/helper.sh", "scripts/run.sh"} {
					if !strings.Contains(string(res.Rewritten), f) {
						t.Fatalf("rewritten %s does not name %q", res.Rewritten, f)
					}
				}
				stored := skill.Content
				for _, f := range skill.Files {
					stored += f.Content
				}
				for _, bad := range []string{"~/" + dot, "$HOME", "${HOME}", resolvedHome(t, m), root} {
					if strings.Contains(stored, bad) {
						t.Fatalf("stored skill still holds %q:\n%s", bad, stored)
					}
				}
				// A skill row carries no warning.
				for _, path := range []string{"/api/skills", "/api/skills/" + skill.ID} {
					if raw := s.rawDo(t, "GET", path, nil, 200); strings.Contains(string(raw), `"warnings"`) {
						t.Fatalf("GET %s has a warnings field: %s", path, raw)
					}
				}
			})
		}
	}
}

func TestSkillHome_TokenIsReplacedAtPlacement(t *testing.T) {
	for _, p := range _providers {
		for _, dot := range []string{".claude", ".codex"} {
			t.Run(p+dot, func(t *testing.T) {
				m, s, _, skill, _ := importOwnRefs(t, p, dot)
				var b platformBot
				s.do(t, "POST", "/api/bots", map[string]any{"name": "Skilled", "persona": "A guide.", "provider": p, "skills": []string{skill.ID}}, &b, 201)
				th := s.newThread(t, b.bot)
				ch := s.events(t, th)
				r := s.send(t, th, "Run the poteto-mode script scripts/check.")
				if got := s.finished(t, ch, r.ID); got.Status != "done" {
					t.Fatalf("run: %+v", got)
				}

				inventory := skillInventories(t, m, p)
				placed := potetoCopy(t, inventory[len(inventory)-1])
				dir := placedDir(m, p, r.ID)
				if filepath.Dir(placed.Path) != dir {
					t.Fatalf("placed at %s, want %s", filepath.Dir(placed.Path), dir)
				}
				want := map[string]string{
					"SKILL.md":          strings.ReplaceAll(_ownSkillMD, _skillDir, dir),
					"notes.md":          "See " + dir + "/notes.md and " + dir + "/literal.md\n",
					"scripts/check":     _checkScript,
					"scripts/helper.sh": "#!/bin/sh\nexec \"" + dir + "/scripts/check\"\n",
					"scripts/run.sh":    "#!/bin/sh\nexec " + dir + "/scripts/helper.sh \"$@\"\n",
				}
				if len(placed.Files) != len(want) {
					t.Fatalf("placed %d files, want %d:\n%s", len(placed.Files), len(want), placedNames(placed.Files))
				}
				for _, f := range placed.Files {
					if f.Text != want[f.Path] {
						t.Fatalf("placed %s is %q, want %q", f.Path, f.Text, want[f.Path])
					}
				}
				if got := onlySkill(t, s); got.Content != _ownSkillMD {
					t.Fatalf("the stored skill changed with the run: %q", got.Content)
				}
			})
		}
	}
}

func TestSkillHome_RefusesOtherHomeRefs(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := startResolved(t, p)
			home := m.home
			tests := []struct {
				name  string
				files []fixtureFile
				refs  []refusal
			}{
				{"other-skill", []fixtureFile{{path: "SKILL.md", content: skillMD("other-skill", "Intro.\nUse ~/.claude/skills/other/x now.\n")}},
					[]refusal{{"SKILL.md", 6, "~/.claude/skills/other/x"}}},
				{"ordered", []fixtureFile{
					{path: "SKILL.md", content: skillMD("ordered", "Clean.\n")},
					{path: "scripts/run.sh", content: "#!/bin/sh\ncd $HOME\necho ok\ncat ~/.ssh/config\n", exec: true},
					{path: "notes/a.md", content: "# Notes\nPlain text.\nSet up in ${HOME} first.\n"},
				}, []refusal{{"notes/a.md", 3, "${HOME}"}, {"scripts/run.sh", 2, "$HOME"}, {"scripts/run.sh", 4, "~/.ssh/config"}}},
				{"home-paths", []fixtureFile{{path: "SKILL.md", content: skillMD("home-paths", "Read $HOME/.config/tool.toml and ${HOME}/.netrc please.\n")}},
					[]refusal{{"SKILL.md", 5, "$HOME/.config/tool.toml"}, {"SKILL.md", 5, "${HOME}/.netrc"}}},
				{"absolute", []fixtureFile{{path: "SKILL.md", content: skillMD("absolute", "Keys live in "+home+"/.aws/credentials today.\n")}},
					[]refusal{{"SKILL.md", 5, home + "/.aws/credentials"}}},
				{"fenced", []fixtureFile{{path: "SKILL.md", content: skillMD("fenced", "Example:\n```sh\ncp x ~/bin/tool\n```\n")}},
					[]refusal{{"SKILL.md", 7, "~/bin/tool"}}},
				{"tidy", []fixtureFile{{path: "SKILL.md", content: skillMD("tidy", "Run ~/.claude/skills/tidy-v2/run now.\nOwn: ~/.claude/skills/tidy/ok now.\n")}},
					[]refusal{{"SKILL.md", 5, "~/.claude/skills/tidy-v2/run"}}},
			}
			codex := filepath.Join(m.home, ".codex", "skills", "codexy")
			c := newImporter(t, s)
			t.Run("other-root", func(t *testing.T) {
				writeFixture(t, codex, []fixtureFile{{path: "SKILL.md", content: skillMD("codexy", "Own: ~/.codex/skills/codexy/run now.\nOther root: ~/.claude/skills/codexy/run now.\n")}})
				imp := c.say(t, "Import the skill folder "+codex)
				if want := []refusal{{"SKILL.md", 6, "~/.claude/skills/codexy/run"}}; imp.res.Status != "refused" || !reflect.DeepEqual(imp.res.Refs, want) {
					t.Fatalf("import: %q %+v, want refused with %+v: only the root it came from is its own folder", imp.summary, imp.res, want)
				}
				if skills := getSkills(t, s); len(skills) != 0 {
					t.Fatalf("library %+v, want nothing stored", skills)
				}
			})
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					root := filepath.Join(m.home, ".claude", "skills", tt.name)
					writeFixture(t, root, tt.files)
					before := treeSum(t, root)

					imp := c.say(t, "Import the skill folder "+root)
					if imp.res.Status != "refused" || imp.res.Name != tt.name || imp.res.Skill != nil {
						t.Fatalf("import: %q %+v, want refused for %q and no skill", imp.summary, imp.res, tt.name)
					}
					if !reflect.DeepEqual(imp.res.Refs, tt.refs) {
						t.Fatalf("refs %+v, want %+v", imp.res.Refs, tt.refs)
					}
					want := []string{`skill "` + tt.name + `"`}
					for _, r := range tt.refs {
						want = append(want, fmt.Sprintf("%s:%d %s", r.File, r.Line, r.Ref))
					}
					for _, text := range []string{imp.res.Reason, imp.summary} {
						for _, w := range want {
							if !strings.Contains(text, w) {
								t.Fatalf("%q does not name %q", text, w)
							}
						}
					}
					if !strings.HasPrefix(imp.summary, "Import refused: ") {
						t.Fatalf("Chief's reply %q, want it to say the import was refused", imp.summary)
					}
					noSkillChange(t, imp.events)
					if skills := getSkills(t, s); len(skills) != 0 {
						t.Fatalf("library %+v, want nothing stored", skills)
					}
					if treeSum(t, root) != before {
						t.Fatal("import changed the source folder")
					}
				})
			}
		})
	}
}

func TestSkillHome_NotHomeReferences(t *testing.T) {
	m, s := startResolved(t, "claude")
	root := filepath.Join(m.home, ".claude", "skills", "plain")
	text := "Docs: https://x/~/a and a~/b, 1~/c, a.~/d, a_~/e, a-~/f, ~~/g, /~/h, $HOMEWORK, $HOME_X, ${HOMEWORK}, ${HOME_X}, ~5, ~3 minutes and a~b.\n" +
		"Ends: a~\n1~\nx.~\na_~\na-~\n/~\n~~strike~~ done\n"
	writeFixture(t, root, []fixtureFile{
		{path: "SKILL.md", content: skillMD("plain", text)},
		{path: "notes.md", content: "A URL https://x/~/a.\n"},
	})
	imp := newImporter(t, s).say(t, "Import the skill folder "+root)
	if imp.res.Status != "created" {
		t.Fatalf("import: %q %+v, want created: none of these is a home reference", imp.summary, imp.res)
	}
	if got := onlySkill(t, s); got.Content != skillMD("plain", text) || len(got.Files) != 1 || got.Files[0].Content != "A URL https://x/~/a.\n" {
		t.Fatalf("stored %+v, want the folder byte for byte", got)
	}
}

func TestSkillHome_ReimportRefusedKeepsStored(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := startResolved(t, p)
			root := filepath.Join(m.home, ".claude", "skills", "tidy")
			writeFixture(t, root, []fixtureFile{
				{path: "SKILL.md", content: skillMD("tidy", "Run ~/.claude/skills/tidy/scripts/run.sh first.\n")},
				{path: "scripts/run.sh", content: "#!/bin/sh\necho tidy\n", exec: true},
			})
			c := newImporter(t, s)
			if created := c.say(t, "Import the skill folder "+root); created.res.Status != "created" {
				t.Fatalf("import: %q %+v", created.summary, created.res)
			}
			skill := onlySkill(t, s)

			// Edited in Reef, so a good re-import would ask; a bad one is refused before it asks.
			edit := map[string]any{"name": "tidy", "content": skill.Content, "files": append(skill.Files, skillFile{Path: "reef.md", Content: "Edited in Reef.\n"})}
			s.do(t, "PUT", "/api/skills/"+skill.ID, edit, nil, 200)
			raw := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200)

			writeFixture(t, root, []fixtureFile{
				{path: "SKILL.md", content: skillMD("tidy", "Run ~/.claude/skills/tidy/scripts/run.sh later.\n")},
				{path: "scripts/run.sh", content: "#!/bin/sh\necho tidy\ncat ~/.ssh/config\n", exec: true},
			})
			sum := treeSum(t, root)
			want := []refusal{{"scripts/run.sh", 3, "~/.ssh/config"}}

			for _, replace := range []bool{false, true} {
				res := reimport(t, s, skill.ID, replace)
				if res.Status != "refused" || res.Name != "tidy" || res.Skill != nil || !reflect.DeepEqual(res.Refs, want) {
					t.Fatalf("reimport (replace %v): %+v, want refused with %+v", replace, res, want)
				}
				if got := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200); string(got) != string(raw) {
					t.Fatalf("refused reimport changed the skill:\nbefore %s\nafter  %s", raw, got)
				}
			}
			again := c.say(t, "Import the skill folder "+root)
			if !reflect.DeepEqual(again.res, reimport(t, s, skill.ID, false)) || again.res.Status != "refused" {
				t.Fatalf("import_skill %+v: want the answer reimport gives, refused", again.res)
			}
			noSkillChange(t, again.events)
			if got := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200); string(got) != string(raw) {
				t.Fatalf("refused import changed the skill:\nbefore %s\nafter  %s", raw, got)
			}
			if treeSum(t, root) != sum {
				t.Fatal("reimport changed the source folder")
			}
		})
	}
}

// The folder rule runs first: a skill whose source resolves outside ~/.claude/skills and
// ~/.codex/skills gets the folder error (400), not the refused status.
func TestSkillHome_FolderRuleComesFirst(t *testing.T) {
	m, s := startResolved(t, "claude")
	root := filepath.Join(m.home, ".claude", "skills", "tidy")
	writeFixture(t, root, []fixtureFile{{path: "SKILL.md", content: skillMD("tidy", "Clean.\n")}})
	newImporter(t, s).say(t, "Import the skill folder "+root)
	skill := onlySkill(t, s)
	raw := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200)

	elsewhere := filepath.Join(m.home, "elsewhere", "tidy")
	writeFixture(t, elsewhere, []fixtureFile{{path: "SKILL.md", content: skillMD("tidy", "Read ~/.ssh/config now.\n")}})
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, root); err != nil {
		t.Fatal(err)
	}

	s.rawDo(t, "POST", "/api/skills/"+skill.ID+"/reimport", []byte(`{"replace":false}`), 400)
	if got := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200); string(got) != string(raw) {
		t.Fatalf("rejected reimport changed the skill:\nbefore %s\nafter  %s", raw, got)
	}
}

func TestPrompt_ChiefImportRefusal(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			system := heySystem(t, s, p, chief(t, s).bot)
			rule := strings.Join(section(t, platformBlock(t, system), _importHeading), "\n")
			if !strings.Contains(rule, "refused") || strings.Contains(rule, "flagged") {
				t.Fatalf("%q must say what import_skill refused, not flagged:\n%s", _importHeading, rule)
			}
		})
	}
}

// A skill folder may be a symlink to a sibling direct child of the skills folder. Its own folder
// is the entry the owner imported from, and the resolved folder counts too; another sibling does not.
func TestSkillHome_SymlinkedEntryIsTheOwnFolder(t *testing.T) {
	for _, p := range _providers {
		for _, dot := range []string{".claude", ".codex"} {
			t.Run(p+dot+"/rewrites", func(t *testing.T) {
				m, s := startResolved(t, p)
				skills := filepath.Join(m.home, dot, "skills")
				entry := filepath.Join(skills, "x")
				folder := "/" + dot + "/skills/"
				writeFixture(t, filepath.Join(skills, "actual"), []fixtureFile{
					{path: "SKILL.md", content: skillMD("x", "Entry: `~"+folder+"x/scripts/run.sh`\n"+
						"Home: $HOME"+folder+"x/scripts/run.sh\n"+
						"Braces: ${HOME}"+folder+"x/scripts/run.sh\n"+
						"Absolute: "+entry+"/scripts/run.sh\n"+
						"Resolved: ~"+folder+"actual/scripts/run.sh\n"+
						"Resolved absolute: "+filepath.Join(skills, "actual")+"/scripts/run.sh\n")},
					{path: "scripts/run.sh", content: "#!/bin/sh\nexec ~" + folder + "x/scripts/helper.sh\n", exec: true},
				})
				if err := os.Symlink("actual", entry); err != nil {
					t.Fatal(err)
				}

				imp := newImporter(t, s).say(t, "Import the skill folder "+entry)
				if imp.res.Status != "created" {
					t.Fatalf("import: %q %+v, want created: every ref is to the skill's own folder", imp.summary, imp.res)
				}
				want := "---\nname: x\ndescription: Test skill.\n---\n" +
					"Entry: `{{ATOLL_SKILL_DIR}}/scripts/run.sh`\n" +
					"Home: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n" +
					"Braces: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n" +
					"Absolute: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n" +
					"Resolved: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n" +
					"Resolved absolute: {{ATOLL_SKILL_DIR}}/scripts/run.sh\n"
				skill := onlySkill(t, s)
				wantFiles := []skillFile{{Path: "scripts/run.sh", Content: "#!/bin/sh\nexec {{ATOLL_SKILL_DIR}}/scripts/helper.sh\n", Executable: true}}
				if skill.Name != "x" || skill.Source != entry || skill.Content != want || !reflect.DeepEqual(skill.Files, wantFiles) {
					t.Fatalf("stored %+v, want name x, source %s, content %q, files %+v", skill, entry, want, wantFiles)
				}
			})
			t.Run(p+dot+"/refuses-other-sibling", func(t *testing.T) {
				m, s := startResolved(t, p)
				skills := filepath.Join(m.home, dot, "skills")
				entry := filepath.Join(skills, "x")
				folder := "/" + dot + "/skills/"
				writeFixture(t, filepath.Join(skills, "actual"), []fixtureFile{{path: "SKILL.md", content: skillMD("x",
					"Own: ~"+folder+"x/run now.\nSibling: ~"+folder+"other/run now.\n")}})
				writeFixture(t, filepath.Join(skills, "other"), []fixtureFile{{path: "SKILL.md", content: skillMD("other", "Other.\n")}})
				if err := os.Symlink("actual", entry); err != nil {
					t.Fatal(err)
				}

				imp := newImporter(t, s).say(t, "Import the skill folder "+entry)
				want := []refusal{{"SKILL.md", 6, "~" + folder + "other/run"}}
				if imp.res.Status != "refused" || !reflect.DeepEqual(imp.res.Refs, want) {
					t.Fatalf("import: %q %+v, want refused with %+v", imp.summary, imp.res, want)
				}
				if skills := getSkills(t, s); len(skills) != 0 {
					t.Fatalf("library %+v, want nothing stored", skills)
				}
			})
		}
	}
}

// TestSkillHome_PlacedCommandsRun runs a Bot whose skill script calls its own folder the way the
// owner writes it, unquoted (`bash ~/.claude/skills/x/run.sh`, `. ~/.claude/skills/x/lib.sh`), from
// Reef's data dir under a name a shell would split or expand. The rule is the outcome, not the
// mechanism: the placed line is made of characters a shell leaves alone, and it runs the run's copy.
func TestSkillHome_PlacedCommandsRun(t *testing.T) {
	const safeLine = `^bash (/[A-Za-z0-9_./:@%+=,-]+)/scripts/run\.sh$`
	tests := []struct{ name, data string }{
		{"plain", "Atoll"},
		{"space", "Application Support"},
		{"quote", "App's Support"},
		{"dollar", "Cash$HOME x"},
		{"meta", "a;b&c(d)*e"},
		{"double-quote-and-backtick", "dq\"bt`x"},
	}
	for _, p := range _providers {
		for _, tt := range tests {
			t.Run(p+"/"+tt.name, func(t *testing.T) {
				m := newMachine(t, p)
				m.home = resolvedHome(t, m)
				m.data = filepath.Join(m.data, tt.data)
				if err := os.MkdirAll(m.data, 0o755); err != nil {
					t.Fatal(err)
				}
				m.signIn(t, p)
				s := m.serve(t)
				root := filepath.Join(m.home, ".claude", "skills", "poteto-mode")
				writeFixture(t, root, []fixtureFile{
					{path: "SKILL.md", content: skillMD("poteto-mode", "Run: bash ~/.claude/skills/poteto-mode/scripts/run.sh\n")},
					{path: "scripts/check", content: "#!/bin/sh\nbash ~/.claude/skills/poteto-mode/scripts/run.sh\n", exec: true},
					{path: "scripts/run.sh", content: "#!/bin/sh\n. ~/.claude/skills/poteto-mode/lib.sh\necho \"run-ok-$LIBVAR in $(cd \"$(dirname \"$0\")\" && pwd -P)\"\n", exec: true},
					{path: "lib.sh", content: "LIBVAR=lib-ok\n"},
				})
				if imp := newImporter(t, s).say(t, "Import the skill folder "+root); imp.res.Status != "created" {
					t.Fatalf("import: %q %+v", imp.summary, imp.res)
				}
				var b platformBot
				s.do(t, "POST", "/api/bots", map[string]any{"name": "Skilled", "persona": "A guide.", "provider": p, "skills": []string{onlySkill(t, s).ID}}, &b, 201)
				th := s.newThread(t, b.bot)
				ch := s.events(t, th)
				r := s.send(t, th, "Run the poteto-mode script scripts/check.")
				got := s.finished(t, ch, r.ID)

				inventory := skillInventories(t, m, p)
				placed := potetoCopy(t, inventory[len(inventory)-1])
				var check string
				for _, f := range placed.Files {
					if f.Path == "scripts/check" {
						check = f.Text
					}
				}
				line := strings.TrimSpace(strings.TrimPrefix(check, "#!/bin/sh\n"))
				if !regexp.MustCompile(safeLine).MatchString(line) {
					t.Fatalf("placed script line %q (data dir %q), want bash <path made of safe characters>/scripts/run.sh", line, tt.data)
				}
				if tt.name == "plain" && !strings.HasPrefix(line, "bash "+placedDir(m, p, r.ID)+"/") {
					t.Fatalf("placed script line %q, want the run copy's own path %s", line, placedDir(m, p, r.ID))
				}
				_, out, _ := strings.Cut(got.Reply, ": ")
				ran, ok := strings.CutPrefix(out, "run-ok-lib-ok in ")
				if got.Status != "done" || !ok {
					t.Fatalf("run: %+v, want done with the script's output run-ok-lib-ok in <folder>", got)
				}
				if ran == "" || strings.HasPrefix(ran, m.home) {
					t.Fatalf("the script ran from %q, want the run's copy, not the owner's home %s", ran, m.home)
				}
			})
		}
	}
}

// Static scan: these forms are home references and refuse. The dynamic forms (`$(echo ~)`, `HO""ME`,
// a `~` and `/` split by a line break) are out of scope and pinned neither way.
func TestSkillHome_ScannerRefusesTheWholeClass(t *testing.T) {
	m, s := startResolved(t, "claude")
	tests := []struct{ name, line, ref string }{
		{"default", "Use ${HOME:-x}/.ssh/id now.", "${HOME:-x}/.ssh/id"},
		{"trim", "Use ${HOME%/}/.ssh/id now.", "${HOME%/}/.ssh/id"},
		{"strip", "Use ${HOME#..}/x now.", "${HOME#..}/x"},
		{"caret", "Use ${HOME^}/x now.", "${HOME^}/x"},
		{"bare-tilde", "Then cd ~", "~"},
		{"user", "Read ~root/.ssh/id now.", "~root/.ssh/id"},
		{"pwd", "Go ~+/x now.", "~+/x"},
		{"oldpwd", "Go ~-/x now.", "~-/x"},
		{"replace", "Use ${HOME/a/b}/x now.", "${HOME/a/b}/x"},
		{"offset", "Use ${HOME:0:3} now.", "${HOME:0:3}"},
		{"transform", "Use ${HOME@Q} now.", "${HOME@Q}"},
		{"bare-then-more", "Run cd ~ && ls", "~"},
		{"bare-alone", "~", "~"},
		{"bare-user", "Run cd ~root now.", "~root"},
	}
	c := newImporter(t, s)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(m.home, ".claude", "skills", "scan-"+tt.name)
			writeFixture(t, root, []fixtureFile{{path: "SKILL.md", content: skillMD("scan-"+tt.name, tt.line+"\n")}})
			imp := c.say(t, "Import the skill folder "+root)
			if want := []refusal{{"SKILL.md", 5, tt.ref}}; imp.res.Status != "refused" || !reflect.DeepEqual(imp.res.Refs, want) {
				t.Fatalf("import of %q: %q %+v, want refused with %+v", tt.line, imp.summary, imp.res, want)
			}
		})
	}
	if skills := getSkills(t, s); len(skills) != 0 {
		t.Fatalf("library %+v, want nothing stored", skills)
	}
}

// The home path as an absolute path has boundaries: it must not be the front of a longer path
// (`<home>x/foo`) nor the tail of one (`/mnt<home>/foo`).
func TestSkillHome_AbsoluteHomeBoundaries(t *testing.T) {
	m, s := startResolved(t, "claude")
	home := m.home
	c := newImporter(t, s)

	own := filepath.Join(home, ".claude", "skills", "abs-ok")
	text := "Longer: " + home + "x/foo here\nMounted: /mnt" + home + "/foo here\nMounted own: /mnt" + own + "/scripts/run.sh here\n"
	writeFixture(t, own, []fixtureFile{{path: "SKILL.md", content: skillMD("abs-ok", text)}})
	if imp := c.say(t, "Import the skill folder "+own); imp.res.Status != "created" {
		t.Fatalf("import: %q %+v, want created: neither is the home path", imp.summary, imp.res)
	}
	if got := onlySkill(t, s); got.Content != skillMD("abs-ok", text) {
		t.Fatalf("stored %q, want the text byte for byte: nothing rewritten", got.Content)
	}

	hit := filepath.Join(home, ".claude", "skills", "abs-hit")
	writeFixture(t, hit, []fixtureFile{{path: "SKILL.md", content: skillMD("abs-hit",
		"Slash: "+home+"/x\nEnd: "+home+"\nSpace: "+home+" next\nQuote: \""+home+"\"\n")}})
	imp := c.say(t, "Import the skill folder "+hit)
	want := []refusal{{"SKILL.md", 5, home + "/x"}, {"SKILL.md", 6, home}, {"SKILL.md", 7, home}, {"SKILL.md", 8, home}}
	if imp.res.Status != "refused" || !reflect.DeepEqual(imp.res.Refs, want) {
		t.Fatalf("import: %q %+v, want refused with %+v", imp.summary, imp.res, want)
	}
}

// A refusal is exactly {status, name, reason, refs}: no null lists for the fields it does not have.
func TestSkillHome_RefusedAnswerHasFourKeys(t *testing.T) {
	m, s := startResolved(t, "claude")
	keys := func(raw []byte) []string {
		t.Helper()
		var got map[string]json.RawMessage
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		var out []string
		for k := range got {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	want := []string{"name", "reason", "refs", "status"}

	bad := filepath.Join(m.home, ".claude", "skills", "bad")
	writeFixture(t, bad, []fixtureFile{{path: "SKILL.md", content: skillMD("bad", "Use ~/.ssh/config now.\n")}})
	imp := newImporter(t, s).say(t, "Import the skill folder "+bad)
	_, line, _ := strings.Cut(imp.run.Reply, "\n")
	if got := keys([]byte(line)); !slices.Equal(got, want) {
		t.Fatalf("import_skill answer %s has keys %q, want %q", line, got, want)
	}

	good := filepath.Join(m.home, ".claude", "skills", "good")
	writeFixture(t, good, []fixtureFile{{path: "SKILL.md", content: skillMD("good", "Fine.\n")}})
	c := newImporter(t, s)
	c.say(t, "Import the skill folder "+good)
	id := onlySkill(t, s).ID
	writeFixture(t, good, []fixtureFile{{path: "SKILL.md", content: skillMD("good", "Use ~/.ssh/config now.\n")}})
	raw := s.rawDo(t, "POST", "/api/skills/"+id+"/reimport", []byte(`{"replace":false}`), 200)
	if got := keys(raw); !slices.Equal(got, want) {
		t.Fatalf("reimport answer %s has keys %q, want %q", raw, got, want)
	}
}
