package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// Contract: chief-bot-contract.md.

// _fakeModels are the model ids each fake provider lists, in its order.
var _fakeModels = map[string][]string{
	"claude": {"default", "opus", "sonnet", "reef-probe"},
	"codex":  {"gpt-6.1-sol", "reef-probe-codex"},
}

type chiefRun struct {
	reply, thread, run string
	events             []event
}

// chiefCall has Chief make one platform tool call in a new thread; the reply is the tool's
// result text, or "Error: " and the error text.
func chiefCall(t *testing.T, s *server, tool string, args map[string]any) chiefRun {
	t.Helper()
	th := s.newThread(t, chief(t, s).bot)
	ch := s.events(t, th)
	payload, _ := json.Marshal(map[string]any{"tool": tool, "args": args})
	r := s.send(t, th, "Call a platform tool. "+string(payload))
	events := until(t, ch, r.ID, "run_finished")
	done := s.run(t, r.ID)
	if done.Status != "done" {
		t.Fatalf("Chief %s %s: %+v", tool, payload, done)
	}
	return chiefRun{reply: done.Reply, thread: th.ID, run: r.ID, events: events}
}

func createdID(t *testing.T, c chiefRun) string {
	t.Helper()
	var b bot
	if err := json.Unmarshal([]byte(c.reply), &b); err != nil || b.ID == "" {
		t.Fatalf("create_bot result %q, want the Bot as JSON", c.reply)
	}
	return b.ID
}

// failsListing checks a call failed, named every one of want and recorded no change.
func failsListing(t *testing.T, c chiefRun, want []string) {
	t.Helper()
	if !strings.HasPrefix(c.reply, "Error: ") {
		t.Fatalf("result %q, want a tool error", c.reply)
	}
	var missing []string
	for _, w := range want {
		if !strings.Contains(c.reply, w) {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("error %q does not name %q", c.reply, missing)
	}
	for _, e := range c.events {
		if e.Type == "platform_changed" {
			t.Errorf("failed call recorded a change: %v", e.Data)
		}
	}
}

func onlyChief(t *testing.T, s *server) {
	t.Helper()
	var bots []botInfo
	s.do(t, "GET", "/api/bots", nil, &bots, 200)
	if len(bots) != 1 || bots[0].ID != "chief" {
		t.Fatalf("GET /api/bots: %+v, want only Chief", bots)
	}
}

func TestChiefBot_CreateWithModel(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			if !hasModel(s.providers(t)[p].Models, _probeModel[p], "") {
				t.Fatalf("%s does not list %s", p, _probeModel[p])
			}
			c := chiefCall(t, s, "create_bot", map[string]any{"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "model": _probeModel[p]})
			if got := s.bot(t, createdID(t, c)); got.Name != "Poteto" || got.Model != _probeModel[p] {
				t.Fatalf("GET /api/bots: %+v, want Poteto with model %s", got, _probeModel[p])
			}
		})
	}
}

func TestChiefBot_CreateWithUnknownModelFails(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			c := chiefCall(t, s, "create_bot", map[string]any{"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "model": "gpt-0-nowhere"})
			failsListing(t, c, _fakeModels[p])
			onlyChief(t, s)
		})
	}
}

func TestChiefBot_Look(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			id := createdID(t, chiefCall(t, s, "create_bot", map[string]any{"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "look": "whale"}))
			if got := s.bot(t, id); got.Look != "whale" {
				t.Fatalf("create_bot look whale: GET look %q", got.Look)
			}
			chiefCall(t, s, "edit_bot", map[string]any{"id": id, "look": "otter"})
			if got := s.bot(t, id); got.Look != "otter" {
				t.Fatalf("edit_bot look otter: GET look %q", got.Look)
			}

			failsListing(t, chiefCall(t, s, "edit_bot", map[string]any{"id": id, "look": "dragon"}), _creatures)
			if got := s.bot(t, id); got.Look != "otter" {
				t.Fatalf("edit_bot look dragon changed look to %q", got.Look)
			}
			var bots []botInfo
			s.do(t, "GET", "/api/bots", nil, &bots, 200)
			failsListing(t, chiefCall(t, s, "create_bot", map[string]any{"name": "Dragon", "persona": "x", "provider": p, "look": "dragon"}), _creatures)
			var after []botInfo
			s.do(t, "GET", "/api/bots", nil, &after, 200)
			if len(after) != len(bots) {
				t.Fatalf("create_bot look dragon created a Bot: %+v", after)
			}
		})
	}
}

func TestChiefBot_EditModelAndLookUndo(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			old := _fakeModels[p][0]
			var b botInfo
			s.do(t, "POST", "/api/bots", map[string]string{"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "look": "shark", "model": old}, &b, 201)

			model := chiefCall(t, s, "edit_bot", map[string]any{"id": b.ID, "model": _probeModel[p]})
			modelChange := platformChange(t, model.events, "chief", model.thread, model.run, "bot", b.ID, "edit")
			look := chiefCall(t, s, "edit_bot", map[string]any{"id": b.ID, "look": "otter"})
			lookChange := platformChange(t, look.events, "chief", look.thread, look.run, "bot", b.ID, "edit")
			if got := s.bot(t, b.ID); got.Model != _probeModel[p] || got.Look != "otter" {
				t.Fatalf("after edits: %+v, want model %s, look otter", got, _probeModel[p])
			}

			s.do(t, "POST", "/api/platform/changes/"+lookChange+"/undo", nil, nil, 204)
			if got := s.bot(t, b.ID); got.Model != _probeModel[p] || got.Look != "shark" {
				t.Fatalf("undo look: %+v, want model %s, look shark", got, _probeModel[p])
			}
			s.do(t, "POST", "/api/platform/changes/"+modelChange+"/undo", nil, nil, 204)
			if got := s.bot(t, b.ID); got.Model != old || got.Look != "shark" {
				t.Fatalf("undo model: %+v, want model %s, look shark", got, old)
			}
		})
	}
}

func TestChiefBot_CreateWithoutModel(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			c := chiefCall(t, s, "create_bot", map[string]any{"name": "Plain", "persona": "Answers questions.", "provider": p})
			if got := s.bot(t, createdID(t, c)); got.Name != "Plain" || got.Provider != p || got.Model != "" {
				t.Fatalf("GET /api/bots: %+v, want Plain on %s with no model", got, p)
			}
		})
	}
}

var _otherProvider = map[string]string{"claude": "codex", "codex": "claude"}

func TestChiefBot_EditProviderChecksModel(t *testing.T) {
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	m.signIn(t, "codex")
	s := m.serve(t)
	for _, p := range _providers {
		other, model := _otherProvider[p], _fakeModels[p][1]
		var b botInfo
		s.do(t, "POST", "/api/bots", map[string]string{"name": "Poteto " + p, "persona": "Reviews diffs.", "provider": p, "model": model}, &b, 201)

		failsListing(t, chiefCall(t, s, "edit_bot", map[string]any{"id": b.ID, "provider": other}), _fakeModels[other])
		if got := s.bot(t, b.ID); got.Provider != p || got.Model != model {
			t.Errorf("edit_bot provider %s alone: %+v, want %s with model %s unchanged", other, got, p, model)
		}

		chiefCall(t, s, "edit_bot", map[string]any{"id": b.ID, "provider": other, "model": _probeModel[other]})
		if got := s.bot(t, b.ID); got.Provider != other || got.Model != _probeModel[other] {
			t.Errorf("edit_bot provider %s and model %s: %+v", other, _probeModel[other], got)
		}
	}
}

func TestChiefBot_EditKeepsOldModel(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			var b botInfo
			s.do(t, "POST", "/api/bots", map[string]string{"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "model": "retired-model"}, &b, 201)
			chiefCall(t, s, "edit_bot", map[string]any{"id": b.ID, "name": "Renamed"})
			chiefCall(t, s, "edit_bot", map[string]any{"id": b.ID, "provider": p, "model": "retired-model", "look": "otter"})
			if got := s.bot(t, b.ID); got.Name != "Renamed" || got.Provider != p || got.Model != "retired-model" || got.Look != "otter" {
				t.Fatalf("edits keeping provider and model: %+v, want Renamed, otter, %s with retired-model", got, p)
			}
		})
	}
}

func TestChiefBot_SignedOutProviderSaysSo(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			// Both installed, only p signed in, so Chief runs on p.
			m := newMachine(t, "claude", "codex")
			m.signIn(t, p)
			s := m.serve(t)
			other := _otherProvider[p]
			c := chiefCall(t, s, "create_bot", map[string]any{"name": "Poteto", "persona": "Reviews diffs.", "provider": other, "model": _fakeModels[other][0]})
			failsListing(t, c, nil)
			if !strings.Contains(strings.ToLower(c.reply), "not signed in") {
				t.Errorf("create_bot on signed-out %s: %q, want it to say %s is not signed in", other, c.reply, other)
			}
			onlyChief(t, s)
		})
	}
}
