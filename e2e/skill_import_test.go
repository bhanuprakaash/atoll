package e2e

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: skill-import-contract.md.

const (
	_importHeading = "Import a local skill"
	_importAgent   = "Make a Bot from ~/.claude/agents/lagoon.md"
	_lagoonAgent   = "---\nname: lagoon\ndescription: Greets visitors.\n---\nYou greet visitors to the lagoon. Before you answer, read ~/.claude/skills/reef-greeting/SKILL.md and follow it.\n"
	_lagoonPersona = "You greet visitors to the lagoon. Before you answer, read the reef-greeting skill and follow it."
)

func TestPrompt_ChiefImportRule(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			system := heySystem(t, s, p, chief(t, s).bot)
			rule := strings.Join(section(t, platformBlock(t, system), _importHeading), "\n")
			for _, want := range []string{"create_skill", "SKILL.md", "persona"} {
				if !strings.Contains(rule, want) {
					t.Fatalf("%q does not name %q:\n%s", _importHeading, want, rule)
				}
			}
		})
	}
}

func TestSkills_ChiefImportsLocalSkill(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			local := filepath.Join(m.home, ".claude")
			writeFile(t, filepath.Join(local, "agents", "lagoon.md"), _lagoonAgent)
			writeFile(t, filepath.Join(local, "skills", "reef-greeting", "SKILL.md"), fake.GreetingSkill)
			writeFile(t, filepath.Join(local, "skills", "reef-greeting", "scripts", "greet.sh"), "#!/bin/sh\necho hello\n")

			b := chief(t, s)
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, _importAgent+" using "+p+".")
			events := until(t, ch, r.ID, "run_finished")
			if done := s.run(t, r.ID); done.Status != "done" || done.Reply != "Imported reef-greeting and created lagoon." {
				t.Fatalf("import: %+v", done)
			}
			var skills []librarySkill
			s.do(t, "GET", "/api/skills", nil, &skills, 200)
			if len(skills) != 1 || skills[0].Name != "reef-greeting" || skills[0].Content != fake.GreetingSkill {
				t.Fatalf("library after import: %+v", skills)
			}
			skill := skills[0]
			var bots []platformBot
			s.do(t, "GET", "/api/bots", nil, &bots, 200)
			var lagoon platformBot
			for _, candidate := range bots {
				if candidate.Name == "lagoon" {
					lagoon = candidate
				}
			}
			if lagoon.ID == "" || !slices.Equal(lagoon.Skills, []string{skill.ID}) || lagoon.Provider != p {
				t.Fatalf("imported Bot: %+v", bots)
			}
			if strings.Contains(lagoon.Persona, "~/.claude") || lagoon.Persona != _lagoonPersona {
				t.Fatalf("persona %q, want %q", lagoon.Persona, _lagoonPersona)
			}
			skillChange := platformChange(t, events, b.ID, th.ID, r.ID, "skill", skill.ID, "create")
			botChange := platformChange(t, events, b.ID, th.ID, r.ID, "bot", lagoon.ID, "create")

			// The Bot must work once the user's local folder is gone.
			if err := os.RemoveAll(local); err != nil {
				t.Fatal(err)
			}
			use := s.newThread(t, lagoon.bot)
			useCh := s.events(t, use)
			used := s.send(t, use, _useSkills)
			if got := s.finished(t, useCh, used.ID); got.Status != "done" || got.Reply != "Skills: reef-greeting. Used reef-greeting: Hello from the lagoon." {
				t.Fatalf("run of the imported Bot: %+v", got)
			}
			if system := lastPrompt(t, m, p, "system"); strings.Contains(system, "~/.claude") {
				t.Fatalf("imported Bot's prompt names ~/.claude:\n%s", system)
			}
			inventory := skillInventory(t, m, p)
			if len(inventory) != 1 || inventory[0].Name != "reef-greeting" || strings.HasPrefix(inventory[0].Path, m.home+string(filepath.Separator)) {
				t.Fatalf("placed skills: %+v", inventory)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(inventory[0].Path), "scripts")); !os.IsNotExist(err) {
				t.Fatalf("placed skill has the local scripts folder (stat err %v)", err)
			}

			s.do(t, "POST", "/api/platform/changes/"+botChange+"/undo", nil, nil, 204)
			s.do(t, "GET", "/api/bots", nil, &bots, 200)
			s.do(t, "GET", "/api/skills", nil, &skills, 200)
			if len(bots) != 1 || bots[0].ID != "chief" || len(skills) != 1 || skills[0] != skill {
				t.Fatalf("after Bot undo: %+v %+v, want only Chief and the skill kept", bots, skills)
			}
			s.do(t, "POST", "/api/platform/changes/"+skillChange+"/undo", nil, nil, 204)
			s.do(t, "GET", "/api/skills", nil, &skills, 200)
			if len(skills) != 0 {
				t.Fatalf("after skill undo: %+v, want none", skills)
			}
		})
	}
}
