package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: skill-files-contract.md.

type (
	// importResult is import_skill's result.
	importResult struct {
		Status string `json:"status"`
		Skill  *struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Source string `json:"source"`
			Files  []struct {
				Path       string `json:"path"`
				Executable bool   `json:"executable"`
			} `json:"files"`
		} `json:"skill"`
		Added     []string        `json:"added"`
		Changed   []string        `json:"changed"`
		Removed   []string        `json:"removed"`
		Skipped   []skipped       `json:"skipped"`
		Warning   string          `json:"warning"`
		Name      string          `json:"name"`
		Reason    string          `json:"reason"`
		Refs      []refusal       `json:"refs"`
		Rewritten json.RawMessage `json:"rewritten"`
	}

	// refusal is one home reference a refused import found.
	refusal struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Ref  string `json:"ref"`
	}

	skipped struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}

	// importRun is one Chief run that imported a folder: its reply's sentence, the tool's result
	// the reply carries, and the run's events.
	importRun struct {
		summary string
		res     importResult
		events  []event
		run     runInfo
	}

	// importer is a thread with Chief.
	importer struct {
		s     *server
		chief platformBot
		th    thread
		ch    <-chan event
	}
)

func newImporter(t *testing.T, s *server) importer {
	t.Helper()
	b := chief(t, s)
	th := s.newThread(t, b.bot)
	return importer{s: s, chief: b, th: th, ch: s.events(t, th)}
}

// say sends a message that has Chief call import_skill once.
func (i importer) say(t *testing.T, message string) importRun {
	t.Helper()
	r := i.s.send(t, i.th, message)
	events := until(t, i.ch, r.ID, "run_finished")
	done := i.s.run(t, r.ID)
	if done.Status != "done" {
		t.Fatalf("Chief import %q: %+v", message, done)
	}
	summary, result, ok := strings.Cut(done.Reply, "\n")
	if !ok {
		t.Fatalf("reply %q has no result line", done.Reply)
	}
	out := importRun{summary: summary, events: events, run: done}
	if err := json.Unmarshal([]byte(result), &out.res); err != nil {
		t.Fatalf("import_skill result %q: %v", result, err)
	}
	return out
}

// noSkillChange fails when a run recorded a change to a skill.
func noSkillChange(t *testing.T, events []event) {
	t.Helper()
	for _, e := range events {
		if change, _ := e.Data["change"].(map[string]any); e.Type == "platform_changed" && change["entity"] == "skill" {
			t.Fatalf("run recorded a skill change: %v", e.Data)
		}
	}
}

func getSkills(t *testing.T, s *server) []fileSkill {
	t.Helper()
	var skills []fileSkill
	s.do(t, "GET", "/api/skills", nil, &skills, 200)
	return skills
}

func onlySkill(t *testing.T, s *server) fileSkill {
	t.Helper()
	skills := getSkills(t, s)
	if len(skills) != 1 {
		t.Fatalf("library: %+v, want one skill", skills)
	}
	return skills[0]
}

func sorted(paths []skipped) []skipped {
	out := slices.Clone(paths)
	slices.SortFunc(out, func(a, b skipped) int { return strings.Compare(a.Path, b.Path) })
	return out
}

func TestSkillFiles_ChiefImportsPotetoMode(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			root := filepath.Join(m.home, ".claude", "skills", "poteto-mode")
			fixture := potetoFixture()
			writeFixture(t, root, fixture)
			before := treeSum(t, root)

			c := newImporter(t, s)
			imp := c.say(t, "Import the skill folder "+_potetoDir)
			if imp.res.Status != "created" || imp.summary != "Import created: poteto-mode. Skipped: none." {
				t.Fatalf("import: %q %+v", imp.summary, imp.res)
			}
			if len(imp.res.Added)+len(imp.res.Changed)+len(imp.res.Removed)+len(imp.res.Skipped) != 0 {
				t.Fatalf("a new skill has nothing added, changed, removed or skipped: %+v", imp.res)
			}

			skill := onlySkill(t, s)
			if skill.Name != "poteto-mode" || skill.Source != root {
				t.Fatalf("skill name %q source %q, want poteto-mode from %q", skill.Name, skill.Source, root)
			}
			if got := imp.res.Skill; got == nil || got.ID != skill.ID || got.Name != "poteto-mode" || got.Source != root || len(got.Files) != 22 {
				t.Fatalf("result skill %+v, want the stored skill with 22 files", got)
			}
			if len(skill.Files) != 22 || !reflect.DeepEqual(skill.Files, libraryFiles(fixture)) {
				t.Fatalf("files %+v, want the fixture's 22: scripts/* executable, the rest not", skill.Files)
			}
			for i, f := range skill.Files {
				if got := imp.res.Skill.Files[i]; got.Path != f.Path || got.Executable != f.Executable {
					t.Fatalf("result file %+v, stored %s executable %v", got, f.Path, f.Executable)
				}
			}
			if skill.Content != potetoPlaced() || strings.Contains(skill.Content, "~/.claude") || !strings.Contains(skill.Content, "`S={{ATOLL_SKILL_DIR}}/scripts`") {
				t.Fatalf("SKILL.md %q, want the placement token and no ~/.claude", skill.Content)
			}
			if after := treeSum(t, root); after != before {
				t.Fatal("import changed the source folder")
			}

			change := platformChange(t, imp.events, c.chief.ID, c.th.ID, imp.run.ID, "skill", skill.ID, "create")
			var changes int
			for _, e := range imp.events {
				if e.Type == "platform_changed" {
					changes++
				}
			}
			if changes != 1 {
				t.Fatalf("%d changes, want one", changes)
			}
			s.do(t, "POST", "/api/platform/changes/"+change+"/undo", nil, nil, 204)
			if skills := getSkills(t, s); len(skills) != 0 {
				t.Fatalf("after undo: %+v, want no skill", skills)
			}
		})
	}
}

func TestSkillFiles_ImportSkipsBinariesAndGit(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			root := filepath.Join(m.home, ".claude", "skills", "reef-snaps")
			fixture := []fixtureFile{
				{path: "SKILL.md", content: "---\nname: reef-snaps\ndescription: Takes snapshots.\n---\nTake a snapshot.\n"},
				{path: "scripts/snap.sh", content: "#!/bin/sh\necho snap\n", exec: true},
				{path: "notes.md", content: "Notes.\n"},
				{path: ".git/HEAD", content: "ref: refs/heads/main\n"},
				{path: ".git/config", content: "[core]\n"},
			}
			writeFixture(t, root, fixture)
			png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89"
			for _, path := range []string{"logo.png", "assets/pixel.png"} {
				writeFile(t, filepath.Join(root, filepath.FromSlash(path)), png)
			}

			imp := newImporter(t, s).say(t, "Import the skill folder "+root)
			if imp.res.Status != "created" {
				t.Fatalf("import: %q %+v", imp.summary, imp.res)
			}
			wantSkipped := []skipped{{".git", "git"}, {"assets/pixel.png", "binary"}, {"logo.png", "binary"}}
			if !slices.Equal(sorted(imp.res.Skipped), wantSkipped) {
				t.Fatalf("skipped %+v, want %+v", imp.res.Skipped, wantSkipped)
			}
			for _, want := range []string{".git (git)", "assets/pixel.png (binary)", "logo.png (binary)"} {
				if !strings.Contains(imp.summary, want) {
					t.Fatalf("Chief's reply %q does not name %q", imp.summary, want)
				}
			}
			if skill := onlySkill(t, s); !reflect.DeepEqual(skill.Files, libraryFiles(fixture[:3])) {
				t.Fatalf("stored files %+v, want notes.md and scripts/snap.sh only", skill.Files)
			}
		})
	}
}

func TestSkillFiles_BotRunsPlacedScript(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			files := libraryFiles(potetoFixture())
			b, _ := seedPotetoBot(t, s, p, files)
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, "Run the poteto-mode script scripts/check.")
			got := s.finished(t, ch, r.ID)

			dir := placedDir(m, p, r.ID)
			if want := "Ran " + filepath.Join(dir, "scripts", "check") + ": check-ok"; got.Status != "done" || got.Reply != want {
				t.Fatalf("run: %+v, want the reply %q", got, want)
			}
			inventory := skillInventories(t, m, p)
			placed := potetoCopy(t, inventory[len(inventory)-1])
			if filepath.Dir(placed.Path) != dir || strings.HasPrefix(placed.Path, m.home+string(filepath.Separator)) {
				t.Fatalf("placed at %s, want %s outside HOME", placed.Path, dir)
			}
			checkPlaced(t, placed.Files, filepath.Dir(placed.Path), files)
			folderGone(t, filepath.Join(m.providerDir(p), "runs", r.ID))
		})
	}
}

func TestSkillFiles_RejectsInvalidFiles(t *testing.T) {
	_, s := start(t, "claude")
	seed := []skillFile{{Path: "scripts/check", Content: _checkScript, Executable: true}, {Path: "playbooks/ship.md", Content: "# ship\n"}}
	var skill fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": seed}, &skill, 201)

	ok := `{"path":"ok.md","content":"ok","executable":false}`
	file := func(path string) string {
		return fmt.Sprintf(`{"path":%q,"content":"x","executable":false}`, path)
	}
	tests := []struct{ name, file string }{
		{"parent", file("../x")},
		{"parent inside", file("scripts/../../x")},
		{"absolute", file("/etc/passwd")},
		{"skill md", file("SKILL.md")},
		{"empty", file("")},
		{"dot", file("./x")},
		{"double slash", file("a//b")},
		{"trailing slash", file("a/")},
		{"duplicate", ok},
		{"file and folder", file("ok.md/inner")},
		{"not utf-8", "{\"path\":\"bin.txt\",\"content\":\"\xff\xfe\",\"executable\":false}"},
		{"nul", `{"path":"nul.txt","content":"a\u0000b","executable":false}`},
	}
	for _, tt := range tests {
		// The valid first file must not be stored either: nothing changes.
		body := `{"name":"changed","content":"changed","files":[` + ok + `,` + tt.file + `]}`
		t.Run("PUT/"+tt.name, func(t *testing.T) {
			before := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200)
			s.rawDo(t, "PUT", "/api/skills/"+skill.ID, []byte(body), 400)
			if after := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200); string(after) != string(before) {
				t.Fatalf("skill changed by a rejected PUT:\nbefore %s\nafter  %s", before, after)
			}
		})
		t.Run("POST/"+tt.name, func(t *testing.T) {
			before := s.rawDo(t, "GET", "/api/skills", nil, 200)
			s.rawDo(t, "POST", "/api/skills", []byte(body), 400)
			if after := s.rawDo(t, "GET", "/api/skills", nil, 200); string(after) != string(before) {
				t.Fatalf("library changed by a rejected POST:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func TestSkillFiles_StoreAndReplace(t *testing.T) {
	_, s := start(t, "claude")
	var plain map[string]json.RawMessage
	s.do(t, "POST", "/api/skills", map[string]string{"name": "reef-greeting", "content": fake.GreetingSkill}, &plain, 201)
	if string(plain["files"]) != "[]" || string(plain["source"]) != `""` {
		t.Fatalf("skill without files: files %s source %s, want [] and \"\"", plain["files"], plain["source"])
	}

	files := []skillFile{{Path: "z.md", Content: "z"}, {Path: "scripts/run", Content: "#!/bin/sh\n", Executable: true}, {Path: "a.md", Content: "a"}}
	var skill fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": files}, &skill, 201)
	want := []skillFile{files[2], files[1], files[0]}
	if !reflect.DeepEqual(skill.Files, want) || skill.Source != "" {
		t.Fatalf("created %+v, want files sorted by path: %+v", skill, want)
	}
	var listed []fileSkill
	s.do(t, "GET", "/api/skills", nil, &listed, 200)
	var one fileSkill
	s.do(t, "GET", "/api/skills/"+skill.ID, nil, &one, 200)
	if len(listed) != 2 || !reflect.DeepEqual(one, skill) || !reflect.DeepEqual(listed[slices.IndexFunc(listed, func(k fileSkill) bool { return k.ID == skill.ID })], skill) {
		t.Fatalf("GET: list %+v, one %+v, want %+v", listed, one, skill)
	}

	// Without a files key, files stay; with one, it is the whole new set.
	s.do(t, "PUT", "/api/skills/"+skill.ID, map[string]string{"name": "renamed", "content": potetoPlaced()}, &one, 200)
	if one.Name != "renamed" || !reflect.DeepEqual(one.Files, want) {
		t.Fatalf("PUT without files: %+v, want the same files", one)
	}
	s.do(t, "PUT", "/api/skills/"+skill.ID, map[string]any{"name": "renamed", "content": potetoPlaced(), "files": want[:1]}, &one, 200)
	if !reflect.DeepEqual(one.Files, want[:1]) {
		t.Fatalf("PUT with one file: %+v", one)
	}
	var cleared map[string]json.RawMessage
	s.do(t, "PUT", "/api/skills/"+skill.ID, map[string]any{"name": "renamed", "content": potetoPlaced(), "files": []skillFile{}}, &cleared, 200)
	if string(cleared["files"]) != "[]" {
		t.Fatalf("PUT with no files: files %s, want []", cleared["files"])
	}
}

func TestSkillFiles_SmallerSetIsPlaced(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			files := libraryFiles(potetoFixture())
			b, skill := seedPotetoBot(t, s, p, files)
			smaller := []skillFile{{Path: "scripts/check", Content: _checkScript, Executable: true}, {Path: "playbooks/ship.md", Content: "# ship\nKept.\n"}}
			var put fileSkill
			s.do(t, "PUT", "/api/skills/"+skill.ID, map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": smaller}, &put, 200)
			if !reflect.DeepEqual(put.Files, []skillFile{smaller[1], smaller[0]}) {
				t.Fatalf("PUT: %+v, want only the two files", put.Files)
			}
			if got := onlySkill(t, s); !reflect.DeepEqual(got, put) {
				t.Fatalf("GET %+v after PUT %+v: removed files came back", got, put)
			}

			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, "Run the poteto-mode script scripts/check.")
			if got := s.finished(t, ch, r.ID); got.Status != "done" || !strings.HasSuffix(got.Reply, ": check-ok") {
				t.Fatalf("run: %+v", got)
			}
			inventory := skillInventories(t, m, p)
			last := potetoCopy(t, inventory[len(inventory)-1])
			checkPlaced(t, last.Files, filepath.Dir(last.Path), put.Files)
		})
	}
}

func TestSkillFiles_ReimportWithoutReefEdit(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			root := filepath.Join(m.home, ".claude", "skills", "poteto-mode")
			fixture := potetoFixture()
			writeFixture(t, root, fixture)
			c := newImporter(t, s)
			c.say(t, "Import the skill folder "+_potetoDir)
			created := onlySkill(t, s)
			raw := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200)

			// Nothing differs: nothing is written and no change is recorded.
			same := c.say(t, "Import the skill folder "+root)
			if same.res.Status != "unchanged" || len(same.res.Added)+len(same.res.Changed)+len(same.res.Removed) != 0 {
				t.Fatalf("re-import of an unchanged folder: %q %+v", same.summary, same.res)
			}
			noSkillChange(t, same.events)
			if got := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200); string(got) != string(raw) {
				t.Fatalf("unchanged import wrote:\nbefore %s\nafter  %s", raw, got)
			}

			// Reef never edited the files, so a changed folder replaces them without asking.
			edited := withoutFile(withFile(withFile(fixture, "playbooks/review.md", "# review\nNew.\n"), "playbooks/new.md", "# new\n"), "playbooks/debug.md")
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, edited)
			again := c.say(t, "Import the skill folder "+root)
			if again.res.Status != "replaced" || again.res.Skill == nil || again.res.Skill.ID != created.ID {
				t.Fatalf("re-import: %q %+v, want replaced in place", again.summary, again.res)
			}
			if !slices.Equal(again.res.Added, []string{"playbooks/new.md"}) || !slices.Equal(again.res.Changed, []string{"playbooks/review.md"}) || !slices.Equal(again.res.Removed, []string{"playbooks/debug.md"}) {
				t.Fatalf("added %q changed %q removed %q", again.res.Added, again.res.Changed, again.res.Removed)
			}
			if got := onlySkill(t, s); got.ID != created.ID || got.Source != root || !reflect.DeepEqual(got.Files, libraryFiles(edited)) {
				t.Fatalf("library after replace: %+v, want the folder's files", got)
			}
			change := platformChange(t, again.events, c.chief.ID, c.th.ID, again.run.ID, "skill", created.ID, "edit")
			s.do(t, "POST", "/api/platform/changes/"+change+"/undo", nil, nil, 204)
			if got := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200); string(got) != string(raw) {
				t.Fatalf("undo of the replace:\nwant %s\ngot  %s", raw, got)
			}
		})
	}
}

func TestSkillFiles_ReimportAfterReefEditAsksFirst(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			root := filepath.Join(m.home, ".claude", "skills", "poteto-mode")
			fixture := potetoFixture()
			writeFixture(t, root, fixture)
			c := newImporter(t, s)
			c.say(t, "Import the skill folder "+_potetoDir)
			created := onlySkill(t, s)

			reef := withFile(fixture, "playbooks/ship.md", "# ship\nEdited in Reef.\n")
			s.do(t, "PUT", "/api/skills/"+created.ID, map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": libraryFiles(reef)}, nil, 200)
			raw := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200)

			next := withoutFile(withFile(withFile(fixture, "playbooks/review.md", "# review\nNew.\n"), "playbooks/new.md", "# new\n"), "references/improve-report.md")
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, next)

			ask := c.say(t, "Import the skill folder "+root)
			if ask.res.Status != "confirm" || ask.res.Skill != nil {
				t.Fatalf("re-import over a Reef edit: %q %+v, want confirm and no skill", ask.summary, ask.res)
			}
			// The Reef-edited file would go back to the folder's text, so it is changed too.
			if !slices.Equal(ask.res.Added, []string{"playbooks/new.md"}) || !slices.Equal(ask.res.Changed, []string{"playbooks/review.md", "playbooks/ship.md"}) || !slices.Equal(ask.res.Removed, []string{"references/improve-report.md"}) {
				t.Fatalf("added %q changed %q removed %q", ask.res.Added, ask.res.Changed, ask.res.Removed)
			}
			if !strings.Contains(ask.res.Warning, "Reef") || !strings.Contains(ask.res.Warning, "overwritten") {
				t.Fatalf("warning %q, want that Reef edits would be overwritten", ask.res.Warning)
			}
			for _, want := range []string{"Reef", "playbooks/new.md", "playbooks/review.md", "playbooks/ship.md", "references/improve-report.md"} {
				if !strings.Contains(ask.summary, want) {
					t.Fatalf("Chief's reply %q does not show %q", ask.summary, want)
				}
			}
			noSkillChange(t, ask.events)
			if got := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200); string(got) != string(raw) {
				t.Fatalf("confirm wrote:\nbefore %s\nafter  %s", raw, got)
			}

			yes := c.say(t, "Yes, replace the skill folder "+root)
			if yes.res.Status != "replaced" || yes.res.Skill == nil || yes.res.Skill.ID != created.ID {
				t.Fatalf("after yes: %q %+v, want replaced", yes.summary, yes.res)
			}
			if got := onlySkill(t, s); !reflect.DeepEqual(got.Files, libraryFiles(next)) || got.Source != root {
				t.Fatalf("library after yes: %+v, want the folder's files", got)
			}
			change := platformChange(t, yes.events, c.chief.ID, c.th.ID, yes.run.ID, "skill", created.ID, "edit")
			s.do(t, "POST", "/api/platform/changes/"+change+"/undo", nil, nil, 204)
			if got := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200); string(got) != string(raw) {
				t.Fatalf("undo of the replace did not bring the Reef edit back:\nwant %s\ngot  %s", raw, got)
			}
		})
	}
}

func TestSkillFiles_LiveRunKeepsItsCopy(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			old := libraryFiles(potetoFixture())
			b, skill := seedPotetoBot(t, s, p, old)
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			live := s.send(t, th, "Wait, then run the poteto-mode script scripts/check.")
			ready := waitReady(t, m, s, p, live.ID)

			changed := libraryFiles(withFile(withoutFile(withFile(potetoFixture(), "scripts/check", "#!/bin/sh\necho check-new\n"), "playbooks/ship.md"), "playbooks/extra.md", "# extra\n"))
			s.do(t, "PUT", "/api/skills/"+skill.ID, map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": changed}, nil, 200)
			writeFile(t, ready+".release", "")

			got := s.finished(t, ch, live.ID)
			if want := "Ran " + filepath.Join(placedDir(m, p, live.ID), "scripts", "check") + ": check-ok"; got.Status != "done" || got.Reply != want {
				t.Fatalf("live run: %+v, want the old script's output: %q", got, want)
			}
			inventory := skillInventories(t, m, p)
			first := potetoCopy(t, inventory[len(inventory)-1])
			checkPlaced(t, first.Files, filepath.Dir(first.Path), old)
			folderGone(t, filepath.Join(m.providerDir(p), "runs", live.ID))

			next := s.send(t, th, "Run the poteto-mode script scripts/check.")
			got = s.finished(t, ch, next.ID)
			if want := "Ran " + filepath.Join(placedDir(m, p, next.ID), "scripts", "check") + ": check-new"; got.Status != "done" || got.Reply != want {
				t.Fatalf("next run: %+v, want %q", got, want)
			}
			inventory = skillInventories(t, m, p)
			second := potetoCopy(t, inventory[len(inventory)-1])
			checkPlaced(t, second.Files, filepath.Dir(second.Path), changed)
		})
	}
}

func TestSkillFiles_ImportSkillIsChiefOnly(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			root := filepath.Join(m.home, ".claude", "skills", "poteto-mode")
			writeFixture(t, root, potetoFixture())
			th := s.newThread(t, s.newBot(t, "Plain", p))
			ch := s.events(t, th)
			got := s.finished(t, ch, s.send(t, th, "Import the skill folder "+root).ID)
			// The fake fails the run when the Bot lists import_skill or its guessed call works.
			if got.Status != "done" || got.Reply != "import_skill denied." {
				t.Fatalf("Bot without the platform switch: %+v", got)
			}
			if skills := getSkills(t, s); len(skills) != 0 {
				t.Fatalf("library: %+v, want none", skills)
			}
		})
	}
}
