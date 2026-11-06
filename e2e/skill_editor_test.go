package e2e

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Contract: skill-editor-contract.md.

// reimport calls POST /api/skills/{id}/reimport and returns the decoded result.
func reimport(t *testing.T, s *server, id string, replace bool) importResult {
	t.Helper()
	var res importResult
	s.do(t, "POST", "/api/skills/"+id+"/reimport", map[string]bool{"replace": replace}, &res, 200)
	return res
}

func TestSkillEditor_ReimportEndpoint(t *testing.T) {
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

			// Nothing differs: the same answer import_skill gives, and nothing is written.
			same := reimport(t, s, created.ID, false)
			if same.Status != "unchanged" || len(same.Added)+len(same.Changed)+len(same.Removed) != 0 {
				t.Fatalf("reimport of an unchanged folder: %+v", same)
			}
			if want := c.say(t, "Import the skill folder "+root).res; !reflect.DeepEqual(same, want) {
				t.Fatalf("reimport %+v, import_skill %+v: want the same answer", same, want)
			}
			if !slices.Equal(same.HomePaths, _potetoHomePaths) {
				t.Fatalf("home_paths %q, want %q", same.HomePaths, _potetoHomePaths)
			}
			if got := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200); string(got) != string(raw) {
				t.Fatalf("unchanged reimport wrote:\nbefore %s\nafter  %s", raw, got)
			}

			// Reef never edited the files: a changed folder replaces them without asking.
			edited := withoutFile(withFile(withFile(fixture, "playbooks/review.md", "# review\nNew.\n"), "playbooks/new.md", "# new\n"), "playbooks/debug.md")
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, edited)
			sum := treeSum(t, root)
			replaced := reimport(t, s, created.ID, false)
			if replaced.Status != "replaced" || replaced.Skill == nil || replaced.Skill.ID != created.ID || replaced.Warning != "" {
				t.Fatalf("reimport of a changed folder: %+v, want replaced in place", replaced)
			}
			if !slices.Equal(replaced.Added, []string{"playbooks/new.md"}) || !slices.Equal(replaced.Changed, []string{"playbooks/review.md"}) || !slices.Equal(replaced.Removed, []string{"playbooks/debug.md"}) {
				t.Fatalf("added %q changed %q removed %q", replaced.Added, replaced.Changed, replaced.Removed)
			}
			if got := onlySkill(t, s); got.ID != created.ID || got.Source != root || !reflect.DeepEqual(got.Files, libraryFiles(edited)) {
				t.Fatalf("library after replace: %+v, want the folder's files", got)
			}
			if treeSum(t, root) != sum {
				t.Fatal("reimport changed the source folder")
			}
			if again := reimport(t, s, created.ID, false); again.Status != "unchanged" {
				t.Fatalf("second reimport: %+v, want unchanged", again)
			}

			// Edited in Reef: it asks first, writes nothing, and replaces only with replace true.
			reef := withFile(edited, "playbooks/ship.md", "# ship\nEdited in Reef.\n")
			s.do(t, "PUT", "/api/skills/"+created.ID, map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": libraryFiles(reef)}, nil, 200)
			raw = s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200)
			next := withoutFile(withFile(edited, "playbooks/review.md", "# review\nNewer.\n"), "references/improve-report.md")
			next = append(next, fixtureFile{path: "scripts/extra", content: "#!/bin/sh\necho extra\n", exec: true})
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, next)

			ask := reimport(t, s, created.ID, false)
			if ask.Status != "confirm" || ask.Skill != nil {
				t.Fatalf("reimport over a Reef edit: %+v, want confirm and no skill", ask)
			}
			if !slices.Equal(ask.Added, []string{"scripts/extra"}) || !slices.Equal(ask.Changed, []string{"playbooks/review.md", "playbooks/ship.md"}) || !slices.Equal(ask.Removed, []string{"references/improve-report.md"}) {
				t.Fatalf("added %q changed %q removed %q", ask.Added, ask.Changed, ask.Removed)
			}
			if !strings.Contains(ask.Warning, "Reef") || !strings.Contains(ask.Warning, "overwritten") {
				t.Fatalf("warning %q, want that Reef edits would be overwritten", ask.Warning)
			}
			if want := c.say(t, "Import the skill folder "+root).res; !reflect.DeepEqual(ask, want) {
				t.Fatalf("reimport %+v, import_skill %+v: want the same answer", ask, want)
			}
			if got := s.rawDo(t, "GET", "/api/skills/"+created.ID, nil, 200); string(got) != string(raw) {
				t.Fatalf("confirm wrote:\nbefore %s\nafter  %s", raw, got)
			}

			yes := reimport(t, s, created.ID, true)
			if yes.Status != "replaced" || yes.Skill == nil || yes.Skill.ID != created.ID || yes.Warning != "" {
				t.Fatalf("reimport with replace: %+v, want replaced", yes)
			}
			if !slices.Equal(yes.Added, ask.Added) || !slices.Equal(yes.Changed, ask.Changed) || !slices.Equal(yes.Removed, ask.Removed) {
				t.Fatalf("replaced lists %q %q %q, want the ones it asked about", yes.Added, yes.Changed, yes.Removed)
			}
			if got := onlySkill(t, s); !reflect.DeepEqual(got.Files, libraryFiles(next)) || got.Source != root {
				t.Fatalf("library after replace: %+v, want the folder's files", got)
			}
			if again := reimport(t, s, created.ID, false); again.Status != "unchanged" {
				t.Fatalf("reimport after replace: %+v, want unchanged: the Reef edit is gone", again)
			}
		})
	}
}

func TestSkillEditor_ReimportReportsSkipped(t *testing.T) {
	m, s := start(t, "claude")
	root := filepath.Join(m.home, ".claude", "skills", "reef-snaps")
	writeFixture(t, root, []fixtureFile{
		{path: "SKILL.md", content: "---\nname: reef-snaps\ndescription: Takes snapshots.\n---\nTake a snapshot.\n"},
		{path: ".git/HEAD", content: "ref: refs/heads/main\n"},
	})
	writeFile(t, filepath.Join(root, "logo.png"), "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	newImporter(t, s).say(t, "Import the skill folder "+root)

	res := reimport(t, s, onlySkill(t, s).ID, false)
	if want := []skipped{{".git", "git"}, {"logo.png", "binary"}}; res.Status != "unchanged" || !slices.Equal(sorted(res.Skipped), want) {
		t.Fatalf("reimport: %+v, want unchanged and skipped %+v", res, want)
	}
}

func TestSkillEditor_ReimportRefusals(t *testing.T) {
	_, s := start(t, "claude")
	var skill fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": libraryFiles(potetoFixture())}, &skill, 201)
	raw := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200)

	for _, replace := range []bool{false, true} {
		s.do(t, "POST", "/api/skills/"+skill.ID+"/reimport", map[string]bool{"replace": replace}, nil, 409)
	}
	if got := s.rawDo(t, "GET", "/api/skills/"+skill.ID, nil, 200); string(got) != string(raw) {
		t.Fatalf("refused reimport changed the skill:\nbefore %s\nafter  %s", raw, got)
	}
	s.do(t, "POST", "/api/skills/nope/reimport", map[string]bool{"replace": false}, nil, 404)
}

// A script the editor adds is placed executable and runs.
func TestSkillEditor_BotRunsAddedScript(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			files := append(libraryFiles(potetoFixture()), skillFile{Path: "scripts/hello", Content: "#!/bin/sh\necho hi\n", Executable: true})
			b, _ := seedPotetoBot(t, s, p, files)
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, "Run the poteto-mode script scripts/hello.")
			got := s.finished(t, ch, r.ID)
			if want := "Ran " + filepath.Join(placedDir(m, p, r.ID), "scripts", "hello") + ": hi"; got.Status != "done" || got.Reply != want {
				t.Fatalf("run: %+v, want the reply %q", got, want)
			}
		})
	}
}
