package e2e

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

const (
	_roSQLMD     = "---\nname: ro-sql\ndescription: Read-only SQL.\n---\nRun `{{ATOLL_SKILL_DIR}}/scripts/ro-sql` for every query.\n"
	_roSQLScript = "#!/bin/sh\necho ro-sql-ok\n"
)

func roSQLFiles() []skillFile {
	return []skillFile{
		{Path: "playbooks/query.md", Content: "# query\nAsk first.\n"},
		{Path: "scripts/ro-sql", Content: _roSQLScript, Executable: true},
	}
}

// chiefSkill decodes a create_skill, edit_skill or get_skill result.
func chiefSkill(t *testing.T, c chiefRun) fileSkill {
	t.Helper()
	var sk fileSkill
	if err := json.Unmarshal([]byte(c.reply), &sk); err != nil || sk.ID == "" {
		t.Fatalf("result %q, want the skill as JSON", c.reply)
	}
	return sk
}

func skillByID(t *testing.T, s *server, id string) fileSkill {
	t.Helper()
	var sk fileSkill
	s.do(t, "GET", "/api/skills/"+id, nil, &sk, 200)
	return sk
}

// roSQLCopy is the placed ro-sql of a run's last skill listing, and the folder it must be in.
func roSQLCopy(t *testing.T, m *machine, p, runID string) (fake.SkillCopy, string) {
	t.Helper()
	dir := filepath.Join(m.providerDir(p), "runs", runID)
	if p == "claude" {
		dir = filepath.Join(dir, ".claude", "skills")
	}
	dir = filepath.Join(dir, "ro-sql")
	inventory := skillInventories(t, m, p)
	for _, c := range inventory[len(inventory)-1] {
		if c.Name == "ro-sql" {
			return c, dir
		}
	}
	t.Fatalf("ro-sql not placed: %+v", inventory[len(inventory)-1])
	return fake.SkillCopy{}, ""
}

// placedFiles is a placed folder as path to mode, without SKILL.md.
func placedFiles(files []fake.SkillFile) map[string]uint32 {
	out := map[string]uint32{}
	for _, f := range files {
		if f.Path != "SKILL.md" {
			out[f.Path] = f.Mode
		}
	}
	return out
}

// runRoSQL has a Bot with the skill run scripts/ro-sql and checks the reply names the placed script.
func runRoSQL(t *testing.T, m *machine, s *server, p string, b platformBot) fake.SkillCopy {
	t.Helper()
	th := s.newThread(t, b.bot)
	ch := s.events(t, th)
	r := s.send(t, th, "Run the ro-sql script scripts/ro-sql.")
	got := s.finished(t, ch, r.ID)
	placed, dir := roSQLCopy(t, m, p, r.ID)
	if want := "Ran " + filepath.Join(dir, "scripts", "ro-sql") + ": ro-sql-ok"; got.Status != "done" || got.Reply != want {
		t.Fatalf("run: %+v, want the reply %q", got, want)
	}
	if filepath.Dir(placed.Path) != dir {
		t.Fatalf("placed at %s, want %s", placed.Path, dir)
	}
	return placed
}

func TestChiefSkillFiles_CreateWithScript(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			c := chiefCall(t, s, "create_skill", map[string]any{"name": "ro-sql", "content": _roSQLMD, "files": roSQLFiles()})
			created := chiefSkill(t, c)
			stored := skillByID(t, s, created.ID)
			if stored.Name != "ro-sql" || stored.Content != _roSQLMD || !reflect.DeepEqual(stored.Files, roSQLFiles()) {
				t.Fatalf("stored %+v, want SKILL.md and both files, scripts/ro-sql executable", stored)
			}
			if !reflect.DeepEqual(created.Files, stored.Files) {
				t.Fatalf("result files %+v, stored %+v", created.Files, stored.Files)
			}
			platformChange(t, c.events, "chief", c.thread, c.run, "skill", created.ID, "create")

			var b platformBot
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Sql", "persona": "Reads the database.", "provider": p, "skills": []string{created.ID}}, &b, 201)
			placed := runRoSQL(t, m, s, p, b)
			if want := map[string]uint32{"playbooks/query.md": 0o444, "scripts/ro-sql": 0o555}; !reflect.DeepEqual(placedFiles(placed.Files), want) {
				t.Fatalf("placed %+v, want %v", placed.Files, want)
			}
			// The placement token becomes the run copy's own path.
			if want := filepath.Join(filepath.Dir(placed.Path), "scripts", "ro-sql"); !strings.Contains(placed.Text, want) || strings.Contains(placed.Text, _skillDir) {
				t.Fatalf("placed SKILL.md %q, want %s in place of the token", placed.Text, want)
			}
		})
	}
}

func TestChiefSkillFiles_GetSkill(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	var seeded fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "ro-sql", "content": _roSQLMD, "files": roSQLFiles()}, &seeded, 201)

	c := chiefCall(t, s, "get_skill", map[string]any{"id": seeded.ID})
	got := chiefSkill(t, c)
	if got.ID != seeded.ID || got.Name != "ro-sql" || got.Content != _roSQLMD || !reflect.DeepEqual(got.Files, roSQLFiles()) {
		t.Fatalf("get_skill %+v, want name, content and every file with its executable flag: %+v", got, seeded)
	}
	for _, e := range c.events {
		if e.Type == "platform_changed" {
			t.Fatalf("get_skill recorded a change: %v", e.Data)
		}
	}
	failsListing(t, chiefCall(t, s, "get_skill", map[string]any{"id": "nope"}), nil)
}

func TestChiefSkillFiles_EditSmallerSet(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			var sk fileSkill
			s.do(t, "POST", "/api/skills", map[string]any{"name": "ro-sql", "content": _roSQLMD, "files": roSQLFiles()}, &sk, 201)
			var b platformBot
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Sql", "persona": "Reads the database.", "provider": p, "skills": []string{sk.ID}}, &b, 201)

			smaller := roSQLFiles()[1:]
			c := chiefCall(t, s, "edit_skill", map[string]any{"id": sk.ID, "files": smaller})
			chiefSkill(t, c)
			if got := skillByID(t, s, sk.ID); !reflect.DeepEqual(got.Files, smaller) || got.Content != _roSQLMD {
				t.Fatalf("after edit_skill %+v, want only scripts/ro-sql", got)
			}
			platformChange(t, c.events, "chief", c.thread, c.run, "skill", sk.ID, "edit")
			placed := runRoSQL(t, m, s, p, b)
			if want := map[string]uint32{"scripts/ro-sql": 0o555}; !reflect.DeepEqual(placedFiles(placed.Files), want) {
				t.Fatalf("placed %+v, want %v", placed.Files, want)
			}
		})
	}
}

func TestChiefSkillFiles_EditWithoutFilesKeepsThem(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	var sk fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "ro-sql", "content": _roSQLMD, "files": roSQLFiles()}, &sk, 201)
	content := strings.Replace(_roSQLMD, "every query", "each query", 1)
	chiefSkill(t, chiefCall(t, s, "edit_skill", map[string]any{"id": sk.ID, "content": content}))
	if got := skillByID(t, s, sk.ID); got.Content != content || !reflect.DeepEqual(got.Files, roSQLFiles()) {
		t.Fatalf("after a SKILL.md edit %+v, want the new content and every file", got)
	}
}

func TestChiefSkillFiles_RefusesBadFiles(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	var sk fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "ro-sql", "content": _roSQLMD, "files": roSQLFiles()}, &sk, 201)
	ok := skillFile{Path: "ok.md", Content: "ok"}
	tests := []struct {
		name string
		file skillFile
		want []string
	}{
		{"parent", skillFile{Path: "../x", Content: "x"}, []string{"../x"}},
		{"absolute", skillFile{Path: "/etc/passwd", Content: "x"}, []string{"/etc/passwd"}},
		{"binary", skillFile{Path: "nul.txt", Content: "a\x00b"}, []string{"nul.txt"}},
		{"home", skillFile{Path: "scripts/ro-sql", Content: "#!/bin/sh\ncat ~/.ssh/config\n", Executable: true}, []string{"scripts/ro-sql:2 ~/.ssh/config"}},
	}
	for _, tt := range tests {
		files := []skillFile{ok, tt.file}
		t.Run("create/"+tt.name, func(t *testing.T) {
			before := s.rawDo(t, "GET", "/api/skills", nil, 200)
			failsListing(t, chiefCall(t, s, "create_skill", map[string]any{"name": "other-" + strings.ReplaceAll(tt.name, " ", "-"), "content": _roSQLMD, "files": files}), tt.want)
			if after := s.rawDo(t, "GET", "/api/skills", nil, 200); string(after) != string(before) {
				t.Fatalf("library changed by a refused create_skill:\nbefore %s\nafter  %s", before, after)
			}
		})
		t.Run("edit/"+tt.name, func(t *testing.T) {
			before := s.rawDo(t, "GET", "/api/skills/"+sk.ID, nil, 200)
			failsListing(t, chiefCall(t, s, "edit_skill", map[string]any{"id": sk.ID, "content": "changed", "files": files}), tt.want)
			if after := s.rawDo(t, "GET", "/api/skills/"+sk.ID, nil, 200); string(after) != string(before) {
				t.Fatalf("skill changed by a refused edit_skill:\nbefore %s\nafter  %s", before, after)
			}
		})
	}

	// SKILL.md is checked too.
	home := strings.Replace(_roSQLMD, "for every query.", "for every query.\nKeys are in ~/.ssh/config.", 1)
	before := s.rawDo(t, "GET", "/api/skills/"+sk.ID, nil, 200)
	failsListing(t, chiefCall(t, s, "edit_skill", map[string]any{"id": sk.ID, "content": home}), []string{"SKILL.md:6 ~/.ssh/config"})
	if after := s.rawDo(t, "GET", "/api/skills/"+sk.ID, nil, 200); string(after) != string(before) {
		t.Fatalf("skill changed by a refused SKILL.md:\nbefore %s\nafter  %s", before, after)
	}

	// The Settings editor's PUT has no home-folder check.
	s.do(t, "PUT", "/api/skills/"+sk.ID, map[string]any{"name": "ro-sql", "content": home, "files": roSQLFiles()}, nil, 200)
}

func TestChiefSkillFiles_EditFilesUndo(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	var sk fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "ro-sql", "content": _roSQLMD, "files": roSQLFiles()}, &sk, 201)
	before := s.rawDo(t, "GET", "/api/skills/"+sk.ID, nil, 200)

	next := []skillFile{roSQLFiles()[1], {Path: "scripts/explain", Content: "#!/bin/sh\necho plan\n", Executable: true}}
	c := chiefCall(t, s, "edit_skill", map[string]any{"id": sk.ID, "files": next})
	if got := skillByID(t, s, sk.ID); !reflect.DeepEqual(got.Files, []skillFile{next[1], next[0]}) {
		t.Fatalf("after edit_skill %+v, want scripts/explain and scripts/ro-sql", got.Files)
	}
	change := platformChange(t, c.events, "chief", c.thread, c.run, "skill", sk.ID, "edit")
	s.do(t, "POST", "/api/platform/changes/"+change+"/undo", nil, nil, 204)
	if after := s.rawDo(t, "GET", "/api/skills/"+sk.ID, nil, 200); string(after) != string(before) {
		t.Fatalf("undo did not bring the old set back:\nwant %s\ngot  %s", before, after)
	}
}
