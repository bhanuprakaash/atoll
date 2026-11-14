package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

const _useSkills = "List your skills and use reef-greeting."

type platformBot struct {
	bot
	Skills            []string `json:"skills"`
	CanManagePlatform bool     `json:"can_manage_platform"`
}
type librarySkill struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func chief(t *testing.T, s *server) platformBot {
	t.Helper()
	var bots []platformBot
	s.do(t, "GET", "/api/bots", nil, &bots, 200)
	for _, b := range bots {
		if b.ID == "chief" {
			if b.Name != "Chief" || !b.CanManagePlatform {
				t.Fatalf("Chief: %+v", b)
			}
			return b
		}
	}
	t.Fatalf("fresh data has no Chief: %+v", bots)
	return platformBot{}
}

func TestSkills_ChiefFirstLaunch(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"claude", "codex", "both", "none"} {
		t.Run(p, func(t *testing.T) {
			providers := []string{}
			if p == "both" {
				providers = []string{"claude", "codex"}
			} else if p != "none" {
				providers = append(providers, p)
			}
			m := newMachine(t, providers...)
			for _, provider := range providers {
				m.signIn(t, provider)
			}
			s := m.serve(t)
			b := chief(t, s)
			want := p
			if p == "none" || p == "both" {
				want = "claude"
			}
			if b.Provider != want {
				t.Fatalf("Chief provider %q, want %q", b.Provider, want)
			}
			s.kill()
			again := m.serve(t)
			chief(t, again)
			var bots []platformBot
			again.do(t, "GET", "/api/bots", nil, &bots, 200)
			if len(bots) != 1 {
				t.Fatalf("restart duplicated Chief: %+v", bots)
			}
		})
	}
}

func TestSkills_ChiefCreatesAndUndo(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			m.signIn(t, p)
			s := m.serve(t)
			b := chief(t, s)
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, "Create reef-greeting and a Lagoon Bot using "+p+".")
			events := until(t, ch, r.ID, "run_finished")
			done := s.run(t, r.ID)
			if done.Status != "done" || done.Reply != "Created reef-greeting and Lagoon." {
				t.Fatalf("create: %+v", done)
			}
			var bots []platformBot
			s.do(t, "GET", "/api/bots", nil, &bots, 200)
			var lagoon platformBot
			for _, b := range bots {
				if b.Name == "Lagoon" {
					lagoon = b
				}
			}
			var skills []librarySkill
			s.do(t, "GET", "/api/skills", nil, &skills, 200)
			if len(skills) != 1 || skills[0].Content != fake.GreetingSkill || lagoon.ID == "" || !slices.Equal(lagoon.Skills, []string{skills[0].ID}) || lagoon.Provider != p || lagoon.CanManagePlatform {
				t.Fatalf("created state: %+v %+v", bots, skills)
			}
			skillChange := platformChange(t, events, b.ID, th.ID, r.ID, "skill", skills[0].ID, "create")
			botChange := platformChange(t, events, b.ID, th.ID, r.ID, "bot", lagoon.ID, "create")
			use := s.newThread(t, lagoon.bot)
			useCh := s.events(t, use)
			used := s.send(t, use, _useSkills)
			got := s.finished(t, useCh, used.ID)
			if got.Status != "done" || got.Reply != "Skills: reef-greeting. Used reef-greeting: Hello from the lagoon." {
				t.Fatalf("use: %+v", got)
			}
			var canonical librarySkill
			s.do(t, "GET", "/api/skills/"+skills[0].ID, nil, &canonical, 200)
			if canonical.Content != fake.GreetingSkill {
				t.Fatalf("placement mutated library: %+v", canonical)
			}
			inventory := skillInventory(t, m, p)
			if len(inventory) != 1 || inventory[0].Name != "reef-greeting" {
				t.Fatalf("placed skills: %+v", inventory)
			}
			if p == "claude" {
				calls := m.sessions(t, p)
				if len(calls) != 2 {
					t.Fatalf("claude ran %d sessions, want Chief and Bot", len(calls))
				}
				for _, c := range calls {
					m.assertClaudeIsolated(t, c)
				}
				extra := flagValue(calls[1].argv, "--add-dir")
				if extra == "" || !strings.HasPrefix(inventory[0].Path, filepath.Join(extra, ".claude", "skills")+string(os.PathSeparator)) {
					t.Errorf("placed skill %q not discovered through --add-dir %q", inventory[0].Path, extra)
				}
			}
			if p == "codex" && (strings.Contains(inventory[0].Text, "allowed-tools:") || strings.Contains(inventory[0].Text, "context:")) {
				t.Fatalf("Claude-only frontmatter in Codex copy: %s", inventory[0].Text)
			}
			if p == "claude" && (!strings.Contains(inventory[0].Text, "allowed-tools: Read") || !strings.Contains(inventory[0].Text, "context: fork")) {
				t.Fatalf("Claude fields lost: %s", inventory[0].Text)
			}
			s.do(t, "POST", "/api/platform/changes/"+botChange+"/undo", nil, nil, 204)
			s.do(t, "POST", "/api/platform/changes/"+skillChange+"/undo", nil, nil, 204)
			s.do(t, "GET", "/api/bots", nil, &bots, 200)
			s.do(t, "GET", "/api/skills", nil, &skills, 200)
			if len(bots) != 1 || bots[0].ID != "chief" || len(skills) != 0 {
				t.Fatalf("undo creation: %+v %+v", bots, skills)
			}
			// A restart must preserve the undone state.
			s.kill()
			s = m.serve(t)
			chief(t, s)
			s.do(t, "GET", "/api/skills", nil, &skills, 200)
			if len(skills) != 0 {
				t.Fatal("undone skill returned after restart")
			}
		})
	}
}

func platformChange(t *testing.T, events []event, botID, threadID, runID, kind, id, action string) string {
	t.Helper()
	for _, e := range events {
		if e.Type != "platform_changed" {
			continue
		}
		change, _ := e.Data["change"].(map[string]any)
		if change["entity_id"] != id || change["entity"] != kind {
			continue
		}
		changeID, _ := change["id"].(string)
		if changeID == "" || change["action"] != action || change["undone"] != false || e.Data["source_bot_id"] != botID || e.Data["thread_id"] != threadID || e.Data["run_id"] != runID || change["source_bot_id"] != botID || change["source_thread_id"] != threadID || change["source_run_id"] != runID {
			t.Fatalf("change provenance: %+v", e.Data)
		}
		after, _ := change["after"].(map[string]any)
		if after["id"] != id {
			t.Fatalf("change after snapshot: %+v", change)
		}
		if action == "create" && change["before"] != nil {
			t.Fatalf("create before snapshot: %+v", change)
		}
		if action == "edit" {
			before, _ := change["before"].(map[string]any)
			if before["id"] != id {
				t.Fatalf("edit before snapshot: %+v", change)
			}
		}
		return changeID
	}
	t.Fatalf("missing platform_changed %s %s: %s", kind, id, types(events))
	return ""
}

func TestSkills_EditUndoAndNoPlatformTools(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			m.signIn(t, p)
			s := m.serve(t)
			target := s.newBot(t, "Before", p)
			th := s.newThread(t, target)
			ch := s.events(t, th)
			r := s.send(t, th, "Create a Bot without platform access.")
			done := s.finished(t, ch, r.ID)
			if done.Status != "done" || done.Reply != "No platform tools available." {
				t.Fatalf("platform tools/list leak: %+v", done)
			}
			b := chief(t, s)
			chiefThread := s.newThread(t, b.bot)
			events := s.events(t, chiefThread)
			r = s.send(t, chiefThread, "Edit the Lagoon Bot persona. bot_id="+target.ID)
			seen := until(t, events, r.ID, "run_finished")
			if done := s.run(t, r.ID); done.Status != "done" {
				t.Fatalf("edit: %+v", done)
			}
			id := platformChange(t, seen, b.ID, chiefThread.ID, r.ID, "bot", target.ID, "edit")
			var edited bot
			s.do(t, "GET", "/api/bots/"+target.ID, nil, &edited, 200)
			if edited.Persona != "A lagoon guide." {
				t.Fatalf("edit: %+v", edited)
			}
			s.kill()
			s = m.serve(t)
			s.do(t, "POST", "/api/platform/changes/"+id+"/undo", nil, nil, 204)
			s.do(t, "GET", "/api/bots/"+target.ID, nil, &edited, 200)
			if !reflect.DeepEqual(edited, target) {
				t.Fatalf("undo: %+v, want %+v", edited, target)
			}
		})
	}
}

func seedSkillBot(t *testing.T, s *server, p, content string) (platformBot, librarySkill) {
	t.Helper()
	var skill librarySkill
	s.do(t, "POST", "/api/skills", map[string]string{"name": "reef-greeting", "content": content}, &skill, 201)
	if skill.ID == "" || skill.Content != content {
		t.Fatalf("skill create: %+v", skill)
	}
	var b platformBot
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Skilled", "persona": "A guide.", "provider": p, "skills": []string{skill.ID}}, &b, 201)
	return b, skill
}

func skillInventory(t *testing.T, m *machine, p string) []fake.SkillCopy {
	t.Helper()
	all := skillInventories(t, m, p)
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

func TestSkills_RepoAndBotSkills(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			m.signIn(t, p)
			s := m.serve(t)
			s.do(t, "PUT", "/api/settings", map[string]string{"worktree_root": t.TempDir()}, nil, 204)
			path := t.TempDir()
			gitIn(t, path, "init", "-b", "main")
			text := "---\nname: repo-skill\ndescription: A committed repo skill.\n---\nRepo skill body.\n"
			for _, dir := range []string{".claude", ".agents"} {
				writeFile(t, filepath.Join(path, dir, "skills", "repo-skill", "SKILL.md"), text)
			}
			gitIn(t, path, "add", ".")
			gitIn(t, path, "commit", "-m", "seed skills")
			var repo struct {
				ID string `json:"id"`
			}
			s.do(t, "POST", "/api/repos", map[string]string{"name": "skill-repo", "path": path}, &repo, 201)
			b, _ := seedSkillBot(t, s, p, fake.GreetingSkill)
			var th thread
			s.do(t, "POST", "/api/bots/"+b.ID+"/threads", map[string]string{"repo_id": repo.ID}, &th, 201)
			ch := s.events(t, th)
			r := s.send(t, th, _useSkills)
			got := s.finished(t, ch, r.ID)
			if got.Status != "done" || !strings.Contains(got.Reply, "Skills: reef-greeting, repo-skill.") {
				t.Fatalf("repo discovery: %+v", got)
			}
			inventory := skillInventory(t, m, p)
			if len(inventory) != 2 {
				t.Fatalf("skills: %+v", inventory)
			}
			for _, placed := range inventory {
				if placed.Name == "repo-skill" {
					want := ".agents"
					if p == "claude" {
						want = ".claude"
					}
					if !strings.Contains(placed.Path, filepath.Join(m.data, "threads", th.ID, want, "skills")) {
						t.Fatalf("repo skill not discovered in clean checkout: %s", placed.Path)
					}
				}
			}
			r = s.send(t, th, "Start work, then list your skills and use reef-greeting.")
			got = s.finished(t, ch, r.ID)
			if got.Status != "done" || !strings.Contains(got.Reply, "Skills: reef-greeting, repo-skill.") {
				t.Fatalf("worktree skills: %+v", got)
			}
			for _, placed := range skillInventory(t, m, p) {
				if placed.Name == "repo-skill" && !strings.Contains(placed.Path, "fix-skill-discovery") {
					t.Fatalf("repo skill not read in edit worktree: %s", placed.Path)
				}
			}

			if got := gitIn(t, path, "status", "--porcelain"); got != "" {
				t.Fatalf("Bot skills written into repo: %s", got)
			}
		})
	}
}

func TestSkills_PlacedCopyDropsDisableModelInvocation(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			m.signIn(t, p)
			s := m.serve(t)
			content := strings.Replace(fake.GreetingSkill, "---\nHello", "disable-model-invocation: true\n---\nHello", 1)
			b, skill := seedSkillBot(t, s, p, content)
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, _useSkills)
			if got := s.finished(t, ch, r.ID); got.Status != "done" || !strings.Contains(got.Reply, "Hello from the lagoon.") {
				t.Fatalf("use: %+v", got)
			}
			inventory := skillInventory(t, m, p)
			if len(inventory) != 1 || inventory[0].Name != "reef-greeting" {
				t.Fatalf("placed skills: %+v", inventory)
			}
			placed := inventory[0].Text
			if strings.Contains(placed, "disable-model-invocation") {
				t.Fatalf("placed copy hides skill from the model: %s", placed)
			}
			for _, want := range []string{"name: reef-greeting\n", "description: Give the Reef greeting.\n", "---\nHello from the lagoon.\n"} {
				if !strings.Contains(placed, want) {
					t.Fatalf("placed copy lost %q: %s", want, placed)
				}
			}
			var stored librarySkill
			s.do(t, "GET", "/api/skills/"+skill.ID, nil, &stored, 200)
			if stored.Content != content {
				t.Fatalf("library changed: %q, want %q", stored.Content, content)
			}
		})
	}
}

func TestSkills_ConcurrentRunsAndNextRun(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			m.signIn(t, p)
			s := m.serve(t)
			b, skill := seedSkillBot(t, s, p, fake.GreetingSkill)
			first := s.newThread(t, b.bot)
			ch := s.events(t, first)
			r := s.send(t, first, "Wait, then list your skills and use reef-greeting.")
			var ready string
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				paths, _ := filepath.Glob(filepath.Join(m.bin, p+".ready.*"))
				if len(paths) > 0 {
					ready = paths[0]
					break
				}
				if run := s.run(t, r.ID); run.Status == "failed" {
					t.Fatalf("hold: %+v", run)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if ready == "" {
				t.Fatal("skill discovery barrier never reached")
			}
			var updatedBot platformBot
			s.do(t, "PUT", "/api/bots/"+b.ID, map[string]string{"persona": "A new persona for the next run."}, &updatedBot, 200)
			if updatedBot.Persona != "A new persona for the next run." {
				t.Fatalf("Bot edit: %+v", updatedBot)
			}
			updated := strings.ReplaceAll(fake.GreetingSkill, "Hello from the lagoon.", "Hello from the reef.")
			var changed librarySkill
			s.do(t, "PUT", "/api/skills/"+skill.ID, map[string]string{"name": skill.Name, "content": updated}, &changed, 200)
			if changed.Content != updated {
				t.Fatalf("edit skill: %+v", changed)
			}
			// A different Bot with no selections must discover no library skills while first runs.
			other := s.newBot(t, "Unskilled", p)
			second := s.newThread(t, other)
			secondCh := s.events(t, second)
			two := s.send(t, second, _useSkills)
			result := s.finished(t, secondCh, two.ID)
			if result.Status != "done" || result.Reply != "Skills: . No reef-greeting skill available." {
				t.Fatalf("concurrent selection leak: %+v", result)
			}
			writeFile(t, ready+".release", "")
			result = s.finished(t, ch, r.ID)
			if result.Status != "done" || !strings.Contains(result.Reply, "Hello from the lagoon.") {
				t.Fatalf("active run changed: %+v", result)
			}
			// Same thread resumes with the updated skill on its next run.
			next := s.send(t, first, _useSkills)
			result = s.finished(t, ch, next.ID)
			if result.Status != "done" || !strings.Contains(result.Reply, "Hello from the reef.") {
				t.Fatalf("next run stale: %+v", result)
			}
			if system := lastPrompt(t, m, p, "system"); !strings.Contains(system, "A new persona for the next run.") {
				t.Fatalf("next run Bot settings stale: %s", system)
			}
		})
	}
}

func TestSkills_ChiefConfiguresAndUndoes(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			s := f.s
			b := chief(t, s)
			connectionID, token := f.connect(t)
			target, skill := seedSkillBot(t, s, p, fake.GreetingSkill)
			path := t.TempDir()
			gitIn(t, path, "init", "-b", "main")
			payload, _ := json.Marshal(map[string]string{"bot_id": target.ID, "skill_id": skill.ID, "repo_path": path, "connection_id": connectionID})
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, "Configure platform fixtures. "+string(payload))
			seen := until(t, ch, r.ID, "run_finished")
			if result := s.run(t, r.ID); result.Status != "done" {
				t.Fatalf("configure: %+v", result)
			}
			var edited librarySkill
			s.do(t, "GET", "/api/skills/"+skill.ID, nil, &edited, 200)
			if edited.Content != strings.ReplaceAll(fake.GreetingSkill, "Hello from the lagoon.", "Hello from the reef.") {
				t.Fatalf("skill edit: %+v", edited)
			}
			var repos []registeredRepo
			s.do(t, "GET", "/api/repos", nil, &repos, 200)
			var repoID string
			for _, repo := range repos {
				if repo.Name == "chief-repo" && repo.Path == path {
					repoID = repo.ID
				}
			}
			if repoID == "" {
				t.Fatalf("register repo: %+v", repos)
			}
			var attached struct {
				Connections []string `json:"connections"`
			}
			s.do(t, "GET", "/api/bots/"+target.ID, nil, &attached, 200)
			if !slices.Equal(attached.Connections, []string{connectionID}) {
				t.Fatalf("attach: %+v", attached)
			}
			for _, change := range []struct{ kind, id, action string }{{"bot", target.ID, "edit"}, {"repo", repoID, "create"}, {"skill", skill.ID, "edit"}} {
				id := platformChange(t, seen, b.ID, th.ID, r.ID, change.kind, change.id, change.action)
				s.do(t, "POST", "/api/platform/changes/"+id+"/undo", nil, nil, 204)
			}
			s.do(t, "GET", "/api/skills/"+skill.ID, nil, &edited, 200)
			if edited != skill {
				t.Fatalf("skill edit undo: %+v", edited)
			}
			s.do(t, "GET", "/api/bots/"+target.ID, nil, &attached, 200)
			if len(attached.Connections) != 0 {
				t.Fatalf("attach undo: %+v", attached)
			}
			s.do(t, "GET", "/api/repos", nil, &repos, 200)
			if len(repos) != 1 || repos[0].ID != f.repo.ID {
				t.Fatalf("repo undo: %+v", repos)
			}
			var connection struct {
				Status string `json:"status"`
			}
			s.do(t, "GET", "/api/connections/"+connectionID, nil, &connection, 200)
			if connection.Status != "connected" {
				t.Fatalf("undo detached user sign-in: %+v", connection)
			}
			assertNoSecret(t, token, f.m.data, f.root)
		})
	}
}
