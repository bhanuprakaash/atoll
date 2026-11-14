package e2e

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: chief-read-contract.md.

const (
	_readAllPrompt   = "List every Bot, skill, connection, repo and model."
	_whichBotsPrompt = "Which Bots do I have?"
)

// _readResults is what the fixture MCP server answers for each read tool.
var _readResults = map[string]any{
	"list_bots":        []map[string]any{{"id": "chief", "name": "Chief"}, {"id": "dynamic-bot-id", "name": "Lagoon"}},
	"list_skills":      []map[string]any{{"id": "dynamic-skill-id", "name": "reef-greeting"}},
	"list_connections": []map[string]any{{"id": "dynamic-connection-id", "kind": "gitlab", "instance_url": "https://gitlab.example", "status": "connected"}},
	"list_repos":       []map[string]any{{"id": "dynamic-repo-id", "name": "reef-repo", "path": "/repos/reef"}},
	"list_models":      []map[string]any{{"name": "claude", "label": "Claude Code", "models": []map[string]string{{"id": "reef-probe", "name": "Reef Probe"}}}},
}

// pick keeps only keys of m; an absent key is nil.
func pick(m map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

func TestChiefRead_Tools(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			// Both providers installed, only p signed in: list_models must leave the other out.
			m := newMachine(t, "claude", "codex")
			m.signIn(t, p)
			for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
				if v := os.Getenv(key); v != "" {
					m.env = append(m.env, key+"="+v)
				}
			}
			s := m.serve(t)
			connectionID, _ := (&repoFixture{m: m, s: s}).connect(t)
			path := t.TempDir()
			gitIn(t, path, "init", "-b", "main")
			var repo registeredRepo
			s.do(t, "POST", "/api/repos", map[string]string{"name": "reef-repo", "path": path}, &repo, 201)
			var skill librarySkill
			s.do(t, "POST", "/api/skills", map[string]string{"name": "reef-greeting", "content": fake.GreetingSkill}, &skill, 201)
			model := map[string]string{"claude": "reef-probe", "codex": "reef-probe-codex"}[p]
			s.do(t, "POST", "/api/bots", map[string]any{
				"name": "Lagoon", "persona": "A lagoon guide.", "provider": p, "model": model, "look": "whale",
				"skills": []string{skill.ID}, "repo_access": []string{repo.ID}, "connections": []string{connectionID},
			}, nil, 201)

			b := chief(t, s)
			th := s.newThread(t, b.bot)
			ch := s.events(t, th)
			r := s.send(t, th, _readAllPrompt)
			events := until(t, ch, r.ID, "run_finished")
			// The fake fails the run with "platform tool missing: <name>" when tools/list lacks one.
			done := s.run(t, r.ID)
			if done.Status != "done" {
				t.Fatalf("read run: %+v", done)
			}
			var got map[string][]map[string]any
			if err := json.Unmarshal([]byte(done.Reply), &got); err != nil {
				t.Fatalf("read reply %q: %v", done.Reply, err)
			}

			var bots []map[string]any
			s.do(t, "GET", "/api/bots", nil, &bots, 200)
			botFields := []string{"id", "name", "model", "look", "skills", "repo_access", "connections"}
			want := map[any]map[string]any{}
			for _, b := range bots {
				want[b["id"]] = pick(b, botFields...)
			}
			listed := map[any]map[string]any{}
			for _, b := range got["list_bots"] {
				listed[b["id"]] = pick(b, botFields...)
			}
			if len(bots) != 2 || !reflect.DeepEqual(listed, want) {
				t.Errorf("list_bots %v, want %v", got["list_bots"], want)
			}

			skills := got["list_skills"]
			if len(skills) != 1 || skills[0]["id"] != skill.ID || skills[0]["name"] != "reef-greeting" {
				t.Errorf("list_skills %v, want [{id:%s name:reef-greeting}]", skills, skill.ID)
			}
			for _, sk := range skills {
				if _, ok := sk["content"]; ok {
					t.Errorf("list_skills returns content: %v", sk)
				}
			}

			var connections []map[string]any
			s.do(t, "GET", "/api/connections", nil, &connections, 200)
			connFields := []string{"id", "kind", "instance_url", "status"}
			if len(connections) != 1 || len(got["list_connections"]) != 1 ||
				!reflect.DeepEqual(pick(got["list_connections"][0], connFields...), pick(connections[0], connFields...)) ||
				connections[0]["id"] != connectionID || connections[0]["kind"] != "gitlab" || connections[0]["status"] != "connected" {
				t.Errorf("list_connections %v, want %v", got["list_connections"], connections)
			}

			wantRepos := []map[string]any{{"id": repo.ID, "name": "reef-repo", "path": path}}
			var repos []map[string]any
			for _, rp := range got["list_repos"] {
				repos = append(repos, pick(rp, "id", "name", "path"))
			}
			if !reflect.DeepEqual(repos, wantRepos) {
				t.Errorf("list_repos %v, want %v", got["list_repos"], wantRepos)
			}

			ready := s.providers(t)[p]
			raw, _ := json.Marshal(got["list_models"])
			var models []struct {
				Name   string       `json:"name"`
				Label  string       `json:"label"`
				Models []modelEntry `json:"models"`
			}
			if err := json.Unmarshal(raw, &models); err != nil {
				t.Fatal(err)
			}
			if len(models) != 1 || models[0].Name != p || models[0].Label != ready.Label ||
				!slices.Equal(models[0].Models, ready.Models) || !hasModel(models[0].Models, model, "") {
				t.Errorf("list_models %s, want only signed-in %s with %+v", raw, p, ready.Models)
			}

			for _, e := range events {
				if e.Type == "platform_changed" {
					t.Errorf("read tools emitted platform_changed: %v", e.Data)
				}
			}
			var changes []any
			s.do(t, "GET", "/api/threads/"+th.ID+"/changes", nil, &changes, 200)
			if len(changes) != 0 {
				t.Errorf("read tools recorded changes: %v", changes)
			}
		})
	}
}

// isStatus reports whether c is a provider's sign-in status check, which every run's launch makes.
func isStatus(c *call) bool {
	return slices.Contains(c.argv, "status") && (slices.Contains(c.argv, "auth") || slices.Contains(c.argv, "login"))
}

func TestChiefRead_ListModelsStartsNoProcess(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	m.signIn(t, "codex")
	s := m.serve(t)
	ready := s.providers(t) // as the app does when it opens
	before := map[string]int{}
	for _, p := range _providers {
		before[p] = len(m.calls(t, p))
	}

	c := chiefCall(t, s, "list_models", map[string]any{})
	var models []struct {
		Name   string       `json:"name"`
		Models []modelEntry `json:"models"`
	}
	if err := json.Unmarshal([]byte(c.reply), &models); err != nil {
		t.Fatalf("list_models result %q: %v", c.reply, err)
	}
	if len(models) != 2 || models[0].Name != "claude" || models[1].Name != "codex" ||
		!slices.Equal(models[0].Models, ready["claude"].Models) || !slices.Equal(models[1].Models, ready["codex"].Models) {
		t.Errorf("list_models %s, want both providers with the models GET /api/providers lists", c.reply)
	}

	// The run itself starts its session and its launch's status check; nothing else may start.
	sessions, statuses := 0, 0
	for _, p := range _providers {
		mode := map[string]string{"claude": "-p", "codex": "app-server"}[p]
		for _, cl := range m.calls(t, p)[before[p]:] {
			switch {
			case slices.Contains(cl.argv, mode) && slices.ContainsFunc(cl.stdin, isTurn):
				sessions++
			case isStatus(cl):
				statuses++
			default:
				t.Errorf("a Chief run calling list_models started %s %q", p, cl.argv)
			}
		}
	}
	if sessions != 1 || statuses > sessions {
		t.Errorf("Chief run started %d sessions and %d status checks, want 1 and at most 1", sessions, statuses)
	}
}

// Codex names an MCP tool without its server, so list_bots arrives bare.
func TestChiefRead_CodexReadToolIsPlatform(t *testing.T) {
	t.Parallel()
	_, s := start(t, "codex")
	th := s.newThread(t, chief(t, s).bot)
	ch := s.events(t, th)
	r := s.send(t, th, _whichBotsPrompt)
	var kinds []any
	for _, e := range until(t, ch, r.ID, "run_finished") {
		if e.Type == "tool_call" && e.Data["name"] == "list_bots" {
			kinds = append(kinds, e.Data["kind"])
		}
	}
	if len(kinds) != 1 || kinds[0] != "platform" {
		t.Fatalf("list_bots tool_call kinds %v, want one \"platform\"", kinds)
	}
}

func TestChiefRead_WhichBots(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			s.newBot(t, "Lagoon", p)
			th := s.newThread(t, chief(t, s).bot)
			ch := s.events(t, th)
			got := s.finished(t, ch, s.send(t, th, _whichBotsPrompt).ID)
			if got.Status != "done" || got.Reply != "Bots: Chief, Lagoon." {
				t.Fatalf("which Bots: %+v, want reply %q", got, "Bots: Chief, Lagoon.")
			}
		})
	}
}
