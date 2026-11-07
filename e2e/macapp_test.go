package e2e

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Slice 6 API contract: mac-app-contract.md.

type (
	providerInfo struct {
		Name     string       `json:"name"`
		Label    string       `json:"label"`
		Found    bool         `json:"found"`
		Path     string       `json:"path"`
		Version  string       `json:"version"`
		SignedIn bool         `json:"signed_in"`
		Models   []modelEntry `json:"models"`
	}

	modelEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	signInState struct {
		Status string `json:"status"`
		URL    string `json:"url"`
		Error  string `json:"error"`
	}

	botInfo struct {
		ID           string          `json:"id"`
		Name         string          `json:"name"`
		Provider     string          `json:"provider"`
		Look         string          `json:"look"`
		Model        string          `json:"model"`
		State        string          `json:"state"`
		Activity     string          `json:"activity"`
		LastActivity time.Time       `json:"last_activity"`
		RepoAccess   json.RawMessage `json:"repo_access"`
		Connections  []string        `json:"connections"`
		Pinned       bool            `json:"pinned"`
	}

	threadInfo struct {
		ID           string    `json:"id"`
		BotID        string    `json:"bot_id"`
		Title        string    `json:"title"`
		LastActivity time.Time `json:"last_activity"`
		RunID        string    `json:"run_id"`
		RunStatus    string    `json:"run_status"`
		Approval     *struct {
			ID    string          `json:"id"`
			Tool  string          `json:"tool"`
			Input json.RawMessage `json:"input"`
		} `json:"approval"`
		Run     *runInfo `json:"run"`
		Message *message `json:"message"`
	}

	runTimes struct {
		Status     string         `json:"status"`
		StartedAt  time.Time      `json:"started_at"`
		FinishedAt time.Time      `json:"finished_at"`
		Tools      map[string]int `json:"tools"`
	}
)

var _creatures = []string{"jellyfish", "whale", "penguin", "shark", "starfish", "anglerfish", "sand_dollar", "sponge", "kelp", "shell", "wave", "island", "sun", "sailboat"}

const _noRecordingPrompt = "This message has no recording, so the provider fails."

// eventually polls ok every 100ms until it holds, failing after d.
func eventually(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within %v: %s", d, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func threadTitle(s string) string {
	words := strings.Fields(s)
	if len(words) > 6 {
		words = words[:6]
	}
	if len(words) > 0 {
		words[len(words)-1] = strings.TrimRight(words[len(words)-1], ".,;:!?")
	}
	return strings.Join(words, " ")
}

func (s *server) providers(t *testing.T) map[string]providerInfo {
	t.Helper()
	var ps []providerInfo
	s.do(t, "GET", "/api/providers", nil, &ps, http.StatusOK)
	var names []string
	out := map[string]providerInfo{}
	for _, p := range ps {
		names = append(names, p.Name)
		out[p.Name] = p
	}
	if !slices.Equal(names, []string{"claude", "codex"}) {
		t.Fatalf("GET /api/providers names %q, want [claude codex] in that order", names)
	}
	return out
}

func (s *server) bot(t *testing.T, id string) botInfo {
	t.Helper()
	var b botInfo
	s.do(t, "GET", "/api/bots/"+id, nil, &b, http.StatusOK)
	return b
}

func (s *server) thread(t *testing.T, id string) threadInfo {
	t.Helper()
	var th threadInfo
	s.do(t, "GET", "/api/threads/"+id, nil, &th, http.StatusOK)
	return th
}

// openThread creates a thread with its first message in one call.
func (s *server) openThread(t *testing.T, botID, text string) threadInfo {
	t.Helper()
	var th threadInfo
	s.do(t, "POST", "/api/bots/"+botID+"/threads", map[string]string{"text": text}, &th, http.StatusCreated)
	if th.ID == "" || th.BotID != botID || th.Run == nil || th.Run.ID == "" || th.Message == nil ||
		th.Message.Text != text || th.Message.Role != "user" || th.Title != threadTitle(text) {
		t.Fatalf("POST /api/bots/%s/threads {text} returned %+v; want the thread with title %q, its message and run", botID, th, threadTitle(text))
	}
	return th
}

func hasModel(ms []modelEntry, id, name string) bool {
	return slices.ContainsFunc(ms, func(m modelEntry) bool { return m.ID == id && (name == "" || m.Name == name) })
}

func TestProviders_List(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	s := m.serve(t)

	ps := s.providers(t)
	for _, c := range []struct {
		name, label, version, model, modelName string
		signedIn                               bool
	}{
		{"claude", "Claude Code", "2.1.306", "reef-probe", "Reef Probe", true},
		{"codex", "Codex", "0.166.0", "reef-probe-codex", "Reef Probe Codex", false},
	} {
		p := ps[c.name]
		if !p.Found || p.Path != filepath.Join(m.bin, c.name) || p.Version != c.version || p.Label != c.label || p.SignedIn != c.signedIn {
			t.Errorf("provider %s: %+v; want found at %s, label %q, version %s, signed_in %v",
				c.name, p, filepath.Join(m.bin, c.name), c.label, c.version, c.signedIn)
		}
		if !hasModel(p.Models, c.model, c.modelName) {
			t.Errorf("provider %s models %+v; want the provider's own list, with {%s %s}", c.name, p.Models, c.model, c.modelName)
		}
	}
}

func TestProviders_RefreshFindsNewInstall(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	s := m.serve(t)
	if p := s.providers(t)["codex"]; p.Found || p.Path != "" || p.SignedIn || len(p.Models) != 0 {
		t.Fatalf("codex before install: %+v; want not found, no path, no models", p)
	}

	installFake(t, "codex", m.bin)
	var ps []providerInfo
	s.do(t, "POST", "/api/providers/refresh", nil, &ps, http.StatusOK)
	i := slices.IndexFunc(ps, func(p providerInfo) bool { return p.Name == "codex" })
	if i < 0 || !ps[i].Found || ps[i].Version != "0.166.0" || !hasModel(ps[i].Models, "reef-probe-codex", "") {
		t.Errorf("POST /api/providers/refresh after installing codex: %+v; want codex found, 0.166.0, with its models", ps)
	}
}

func TestProviders_SignIn(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, p)
			s := m.serve(t)
			if s.providers(t)[p].SignedIn {
				t.Fatalf("%s signed in before sign-in", p)
			}
			url := map[string]string{"claude": _claudeLoginURL, "codex": _codexLoginURL}[p]
			path := "/api/providers/" + p + "/sign-in"

			var st signInState
			s.do(t, "POST", path, nil, &st, http.StatusAccepted)
			eventually(t, 10*time.Second, p+" sign-in waiting with the printed https URL", func() bool {
				s.do(t, "GET", path, nil, &st, http.StatusOK)
				return st.Status == "waiting" && st.URL == url
			})
			s.do(t, "POST", path, nil, &st, http.StatusAccepted)
			if st.Status != "waiting" || st.URL != url {
				t.Errorf("second POST %s while waiting: %+v; want the same sign-in", path, st)
			}
			if s.providers(t)[p].SignedIn {
				t.Errorf("%s signed in before the browser step finished", p)
			}

			writeFile(t, filepath.Join(m.bin, p+".login.release"), "")
			eventually(t, 10*time.Second, p+" sign-in finished", func() bool {
				s.do(t, "GET", path, nil, &st, http.StatusOK)
				return st.Status == "signed_in"
			})
			if !s.providers(t)[p].SignedIn {
				t.Errorf("%s not signed in after its login finished", p)
			}

			args := map[string][]string{"claude": {"auth", "login"}, "codex": {"login"}}[p]
			env := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}[p]
			var logins []*call
			for _, c := range m.calls(t, p) {
				if slices.Equal(c.argv, args) {
					logins = append(logins, c)
				}
			}
			if len(logins) != 1 {
				t.Fatalf("%s %q ran %d times, want once", p, args, len(logins))
			}
			if got := logins[0].env[env]; got != m.providerDir(p) {
				t.Errorf("%s login ran with %s=%q, want Atoll's folder %q", p, env, got, m.providerDir(p))
			}
			if got := logins[0].env["HOME"]; got != m.home {
				t.Errorf("%s login ran with HOME=%q, want the user's %q unchanged", p, got, m.home)
			}
			s.do(t, "POST", "/api/providers/nope/sign-in", nil, nil, http.StatusNotFound)
		})
	}
}

func TestBots_LookAndModel(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			if b := s.bot(t, "chief"); b.Look != "jellyfish" {
				t.Errorf("Chief look %q, want jellyfish", b.Look)
			}

			repoPath := t.TempDir()
			gitIn(t, repoPath, "init", "-b", "main")
			writeFile(t, filepath.Join(repoPath, "answer.txt"), "41\n")
			gitIn(t, repoPath, "add", "answer.txt")
			gitIn(t, repoPath, "commit", "-m", "seed")
			var repo registeredRepo
			s.do(t, "POST", "/api/repos", map[string]string{"name": "looks", "path": repoPath}, &repo, http.StatusCreated)

			var b botInfo
			s.do(t, "POST", "/api/bots", map[string]any{
				"name": "Finn", "persona": "Fixes things.", "provider": p, "look": "shark",
				"model": _probeModel[p], "repo_access": []string{repo.ID}, "connections": []string{},
			}, &b, http.StatusCreated)
			got := s.bot(t, b.ID)
			if got.Look != "shark" || got.Model != _probeModel[p] || string(got.RepoAccess) != `["`+repo.ID+`"]` || got.Connections == nil {
				t.Errorf("GET /api/bots/%s: %+v; want look shark, model %s, repo_access [%s], connections []", b.ID, got, _probeModel[p], repo.ID)
			}

			var plain botInfo
			s.do(t, "POST", "/api/bots", map[string]string{"name": "Plain", "persona": "x", "provider": p}, &plain, http.StatusCreated)
			if !slices.Contains(_creatures, plain.Look) {
				t.Errorf("Bot created without a look has look %q, want one of %q", plain.Look, _creatures)
			}
			s.do(t, "POST", "/api/bots", map[string]string{"name": "Bad", "persona": "x", "provider": p, "look": "dragon"}, nil, http.StatusBadRequest)

			var edited botInfo
			s.do(t, "PUT", "/api/bots/"+b.ID, map[string]string{"look": "anglerfish"}, &edited, http.StatusOK)
			if edited.Look != "anglerfish" || edited.Model != _probeModel[p] {
				t.Errorf("PUT look: %+v; want look anglerfish, model %s kept", edited, _probeModel[p])
			}

			th := s.newThread(t, bot{ID: b.ID})
			ev := s.events(t, th)
			s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
			sessions := m.sessions(t, p)
			if len(sessions) == 0 {
				t.Fatalf("%s ran no session", p)
			}
			c := sessions[len(sessions)-1]
			switch p {
			case "claude":
				if got := flagValue(c.argv, "--model"); got != _probeModel[p] {
					t.Errorf("claude run --model %q, want the Bot's model %q", got, _probeModel[p])
				}
			case "codex":
				if got := codexModel(c); got != _probeModel[p] {
					t.Errorf("codex thread/start model %q, want the Bot's model %q", got, _probeModel[p])
				}
			}
		})
	}
}

// codexModel is the model a codex session asked for: thread/start's model, or its config's.
func codexModel(c *call) string {
	for _, line := range c.stdin {
		var r struct {
			Method string `json:"method"`
			Params struct {
				Model  string `json:"model"`
				Config struct {
					Model string `json:"model"`
				} `json:"config"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &r) == nil && (r.Method == "thread/start" || r.Method == "thread/resume") {
			return r.Params.Model + r.Params.Config.Model
		}
	}
	return ""
}

func TestThreads_Summary(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			before := time.Now().Add(-time.Second)

			empty := s.thread(t, s.newThread(t, b).ID)
			if empty.Title != "" || empty.RunStatus != "" || empty.RunID != "" || empty.Approval != nil || empty.LastActivity.Before(before) {
				t.Errorf("thread with no messages: %+v; want no title, no run, no approval, last_activity at creation", empty)
			}

			waiting := s.openThread(t, b.ID, _writePrompt)
			var w threadInfo
			eventually(t, 15*time.Second, "thread waiting on its approval", func() bool {
				w = s.thread(t, waiting.ID)
				return w.RunStatus == "waiting" && w.Approval != nil
			})
			if w.RunID != waiting.Run.ID || w.Approval.ID == "" || w.Approval.Tool == "" || !strings.Contains(string(w.Approval.Input), "hello.txt") {
				t.Errorf("waiting thread: %+v; want run %s and the approval's id, tool and input (hello.txt)", w, waiting.Run.ID)
			}
			if st := s.bot(t, b.ID).State; st != "needs_you" {
				t.Errorf("Bot state %q with a thread waiting on approval, want needs_you", st)
			}

			done := s.openThread(t, b.ID, _keepPrompt)
			eventually(t, 15*time.Second, "second thread's run done", func() bool {
				return s.thread(t, done.ID).RunStatus == "done"
			})
			long := "Привет, это первое сообщение длиннее сорока символов, честно."
			runes := s.openThread(t, b.ID, long)
			if runes.Title != threadTitle(long) {
				t.Errorf("title %q, want the first six words without final punctuation %q", runes.Title, threadTitle(long))
			}

			var list []threadInfo
			s.do(t, "GET", "/api/bots/"+b.ID+"/threads", nil, &list, http.StatusOK)
			byID := map[string]threadInfo{}
			for i, th := range list {
				byID[th.ID] = th
				if i > 0 && th.LastActivity.After(list[i-1].LastActivity) {
					t.Errorf("thread list not newest activity first at %d: %v after %v", i, th.LastActivity, list[i-1].LastActivity)
				}
			}
			for id, want := range map[string]string{empty.ID: "", waiting.ID: threadTitle(_writePrompt), done.ID: threadTitle(_keepPrompt), runes.ID: threadTitle(long)} {
				if got, ok := byID[id]; !ok || got.Title != want || got.LastActivity.Before(before) {
					t.Errorf("listed thread %s: %+v (listed %v); want title %q and last_activity", id, got, ok, want)
				}
			}
			if l := byID[waiting.ID]; l.RunStatus != "waiting" || l.Approval == nil || l.Approval.ID != w.Approval.ID {
				t.Errorf("listed waiting thread: %+v; want run_status waiting with approval %s", l, w.Approval.ID)
			}
			if l := byID[done.ID]; l.RunStatus != "done" || l.Approval != nil {
				t.Errorf("listed done thread: %+v; want run_status done, no approval", l)
			}

			s.do(t, "POST", "/api/approvals/"+w.Approval.ID, map[string]bool{"approve": true}, nil, http.StatusNoContent)
			eventually(t, 15*time.Second, "approved thread done with no approval", func() bool {
				th := s.thread(t, waiting.ID)
				return th.RunStatus == "done" && th.Approval == nil
			})
		})
	}
}

func TestBots_State(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	s := m.serve(t)
	waitState := func(id, want string) botInfo {
		t.Helper()
		var b botInfo
		eventually(t, 15*time.Second, "Bot "+id+" state "+want, func() bool {
			b = s.bot(t, id)
			return b.State == want
		})
		return b
	}

	waitState("chief", "idle")
	idle := s.newBot(t, "Idle", "claude")
	waitState(idle.ID, "idle")
	asleep := s.newBot(t, "Sleeper", "codex") // codex is installed but not signed in
	waitState(asleep.ID, "asleep")

	working := s.newBot(t, "Worker", "claude")
	s.openThread(t, working.ID, _hangPrompt)
	waitState(working.ID, "working")

	asker := s.newBot(t, "Asker", "claude")
	s.openThread(t, asker.ID, _writePrompt)
	waitState(asker.ID, "needs_you")

	failer := s.newBot(t, "Breaker", "claude")
	s.openThread(t, failer.ID, _noRecordingPrompt)
	b := waitState(failer.ID, "failed")
	if b.Activity == "" || b.LastActivity.IsZero() {
		t.Errorf("Bot with a failed run: activity %q, last_activity %v; want both set", b.Activity, b.LastActivity)
	}

	var all []botInfo
	s.do(t, "GET", "/api/bots", nil, &all, http.StatusOK)
	want := map[string]string{"chief": "idle", idle.ID: "idle", asleep.ID: "asleep", working.ID: "working", asker.ID: "needs_you", failer.ID: "failed"}
	for _, b := range all {
		if w, ok := want[b.ID]; ok && b.State != w {
			t.Errorf("GET /api/bots: %s state %q, want %q", b.Name, b.State, w)
		}
	}
}

func TestRun_ToolKinds(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _workingPrompt)
			es := until(t, ev, r.ID, "run_finished")

			var got []string
			for _, e := range es {
				if e.Type != "tool_call" {
					continue
				}
				kind, _ := e.Data["kind"].(string)
				target, _ := e.Data["target"].(string)
				if kind == "" {
					t.Errorf("tool_call %v has no kind", e.Data)
				}
				if slices.Contains([]string{"search", "read", "edit", "command"}, kind) {
					got = append(got, kind+" "+target)
				}
			}
			command := map[string]string{"claude": "command Run the tests", "codex": "command pnpm"}[p]
			want := []string{"search selectedRepo", "read repo-step.tsx", "edit wizard.tsx", command}
			if !slices.Equal(got, want) {
				t.Errorf("tool_call kinds and targets %q, want %q", got, want)
			}

			var rt runTimes
			s.do(t, "GET", "/api/runs/"+r.ID, nil, &rt, http.StatusOK)
			if rt.Status != "done" || rt.StartedAt.IsZero() || rt.FinishedAt.Sub(rt.StartedAt) < 4*_workingPause {
				t.Errorf("run %+v; want done with started_at and finished_at at least %v apart", rt, 4*_workingPause)
			}
			for _, k := range []string{"search", "read", "edit", "command"} {
				if rt.Tools[k] != 1 {
					t.Errorf("run tools %v, want 1 call each of search, read, edit, command (Atoll's own tools not counted)", rt.Tools)
					break
				}
			}
			if got := s.run(t, r.ID).Reply; got != _workingReply {
				t.Errorf("reply %q, want %q", got, _workingReply)
			}
		})
	}
}

// gone reports whether pid no longer runs.
func gone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestRun_Stop(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _hangPrompt)
			until(t, ev, r.ID, "tool_call")
			sessions := m.sessions(t, p)
			pid := sessions[len(sessions)-1].pid

			s.do(t, "POST", "/api/runs/"+r.ID+"/stop", nil, nil, http.StatusNoContent)
			es := until(t, ev, r.ID, "run_finished")
			if st := es[len(es)-1].Data["status"]; st != "stopped" {
				t.Errorf("run_finished status %v after stop, want stopped", st)
			}
			if got := s.run(t, r.ID).Status; got != "stopped" {
				t.Errorf("run status %q after stop, want stopped", got)
			}
			eventually(t, 5*time.Second, "the stopped run's provider process exits", func() bool { return gone(pid) })
			s.do(t, "POST", "/api/runs/"+r.ID+"/stop", nil, nil, http.StatusConflict)
			s.do(t, "POST", "/api/runs/nope/stop", nil, nil, http.StatusNotFound)

			// The thread takes the next message: no run is left going.
			next := s.send(t, th, _keepPrompt)
			s.finished(t, ev, next.ID)
		})
	}
}

func TestRun_StopWhileWaiting(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _writePrompt)
			es := until(t, ev, r.ID, "approval")
			id, _ := es[len(es)-1].Data["id"].(string)

			s.do(t, "POST", "/api/runs/"+r.ID+"/stop", nil, nil, http.StatusNoContent)
			es = until(t, ev, r.ID, "run_finished")
			if st := es[len(es)-1].Data["status"]; st != "stopped" {
				t.Errorf("run_finished status %v after stopping a waiting run, want stopped", st)
			}
			if a := s.thread(t, th.ID).Approval; a != nil {
				t.Errorf("thread still shows approval %+v after stop", a)
			}
			s.do(t, "POST", "/api/approvals/"+id, map[string]bool{"approve": true}, nil, http.StatusNotFound)
		})
	}
}

func TestRestart_ThreadRecovers(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			ev := s.events(t, th)
			s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
			r := s.send(t, th, _writePrompt)
			until(t, ev, r.ID, "approval")

			s.kill()
			s = m.serve(t)

			got := s.thread(t, th.ID)
			if got.RunID != r.ID || got.RunStatus != "failed" || got.Approval != nil || got.Title != threadTitle(_keepPrompt) {
				t.Errorf("thread after a restart: %+v; want run %s failed, no approval, title %q", got, r.ID, threadTitle(_keepPrompt))
			}
			var list []threadInfo
			s.do(t, "GET", "/api/bots/"+b.ID+"/threads", nil, &list, http.StatusOK)
			if !slices.ContainsFunc(list, func(l threadInfo) bool { return l.ID == th.ID }) {
				t.Errorf("thread %s missing from its Bot's list after a restart: %+v", th.ID, list)
			}

			ev = s.events(t, th)
			again := s.finished(t, ev, s.send(t, th, _askPrompt).ID)
			if again.Status != "done" || again.Reply != "kestrel" {
				t.Errorf("message after a restart: status %q, reply %q; want done, kestrel from the resumed session (error %q)",
					again.Status, again.Reply, again.Error)
			}
		})
	}
}
