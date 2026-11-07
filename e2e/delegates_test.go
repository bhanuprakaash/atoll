package e2e

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

// Contract: delegates-contract.md.

// delegates returns a Bot's raw "delegates" as sorted ids; absent or null fails the test.
func delegates(t *testing.T, where string, raw json.RawMessage) []string {
	t.Helper()
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil || ids == nil {
		t.Fatalf("%s delegates %s, want an array of Bot ids", where, raw)
	}
	slices.Sort(ids)
	return ids
}

func botDelegates(t *testing.T, s *server, id string) []string {
	t.Helper()
	var b struct {
		Delegates json.RawMessage `json:"delegates"`
	}
	s.do(t, "GET", "/api/bots/"+id, nil, &b, http.StatusOK)
	return delegates(t, "GET /api/bots/"+id, b.Delegates)
}

func wantDelegates(t *testing.T, s *server, id, when string, want ...string) {
	t.Helper()
	want = append([]string{}, want...)
	slices.Sort(want)
	if got := botDelegates(t, s, id); !slices.Equal(got, want) {
		t.Fatalf("%s: GET /api/bots/%s delegates %q, want %q", when, id, got, want)
	}
}

func TestDelegates_API(t *testing.T) {
	_, s := start(t, "claude")
	poteto := s.newBot(t, "Poteto", "claude")
	lagoon := s.newBot(t, "Lagoon", "claude")
	mako := s.newBot(t, "Mako", "claude")

	var bots []struct {
		ID        string          `json:"id"`
		Delegates json.RawMessage `json:"delegates"`
	}
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	for _, b := range bots {
		if got := delegates(t, "GET /api/bots "+b.ID, b.Delegates); len(got) != 0 {
			t.Fatalf("GET /api/bots %s delegates %q, want []", b.ID, got)
		}
	}
	wantDelegates(t, s, poteto.ID, "POST without delegates")

	var kelp bot
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Kelp", "persona": "x", "provider": "claude", "delegates": []string{lagoon.ID}}, &kelp, http.StatusCreated)
	wantDelegates(t, s, kelp.ID, "POST delegates [Lagoon]", lagoon.ID)

	s.do(t, "PUT", "/api/bots/"+poteto.ID, map[string]any{"delegates": []string{lagoon.ID, mako.ID}}, nil, http.StatusOK)
	wantDelegates(t, s, poteto.ID, "PUT delegates [Lagoon, Mako]", lagoon.ID, mako.ID)
	s.do(t, "PUT", "/api/bots/"+poteto.ID, map[string]any{"persona": "Reviews diffs."}, nil, http.StatusOK)
	wantDelegates(t, s, poteto.ID, "PUT without delegates", lagoon.ID, mako.ID)

	rejected(t, s, "PUT", "/api/bots/"+poteto.ID, map[string]any{"delegates": []string{lagoon.ID, poteto.ID}}, []string{"Lagoon", "Mako"})
	wantDelegates(t, s, poteto.ID, "PUT listing itself", lagoon.ID, mako.ID)
	rejected(t, s, "PUT", "/api/bots/"+poteto.ID, map[string]any{"delegates": []string{"no-such-bot"}}, []string{"Lagoon", "Mako"})
	wantDelegates(t, s, poteto.ID, "PUT an unknown id", lagoon.ID, mako.ID)

	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	rejected(t, s, "POST", "/api/bots", map[string]any{"name": "Reef", "persona": "x", "provider": "claude", "delegates": []string{"no-such-bot"}}, []string{"Poteto", "Lagoon", "Mako", "Kelp"})
	var after []bot
	s.do(t, "GET", "/api/bots", nil, &after, http.StatusOK)
	if len(after) != len(bots) {
		t.Fatalf("POST with an unknown delegate created a Bot: %+v", after)
	}

	s.do(t, "PUT", "/api/bots/"+poteto.ID, map[string]any{"delegates": []string{}}, nil, http.StatusOK)
	wantDelegates(t, s, poteto.ID, "PUT delegates []")
}

func TestDelegates_Chief(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			lagoon := s.newBot(t, "Lagoon", p)
			mako := s.newBot(t, "Mako", p)

			poteto := createdID(t, chiefCall(t, s, "create_bot", map[string]any{"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "delegates": []string{lagoon.ID}}))
			wantDelegates(t, s, poteto, "create_bot delegates [Lagoon]", lagoon.ID)

			list := chiefCall(t, s, "list_bots", map[string]any{})
			var listed []struct {
				ID        string          `json:"id"`
				Delegates json.RawMessage `json:"delegates"`
			}
			if err := json.Unmarshal([]byte(list.reply), &listed); err != nil {
				t.Fatalf("list_bots result %q, want the Bots as JSON", list.reply)
			}
			shown := map[string][]string{}
			for _, b := range listed {
				shown[b.ID] = delegates(t, "list_bots "+b.ID, b.Delegates)
			}
			if !slices.Equal(shown[poteto], []string{lagoon.ID}) || shown[lagoon.ID] == nil || len(shown[lagoon.ID]) != 0 {
				t.Fatalf("list_bots delegates %q, want Poteto [%s] and Lagoon []", shown, lagoon.ID)
			}

			edit := chiefCall(t, s, "edit_bot", map[string]any{"id": poteto, "delegates": []string{lagoon.ID, mako.ID}})
			change := platformChange(t, edit.events, "chief", edit.thread, edit.run, "bot", poteto, "edit")
			wantDelegates(t, s, poteto, "edit_bot delegates [Lagoon, Mako]", lagoon.ID, mako.ID)
			for _, e := range edit.events {
				if e.Type != "platform_changed" {
					continue
				}
				var c struct {
					Before, After struct {
						Delegates json.RawMessage `json:"delegates"`
					}
				}
				raw, _ := json.Marshal(e.Data["change"])
				_ = json.Unmarshal(raw, &c)
				before, after := delegates(t, "change before", c.Before.Delegates), delegates(t, "change after", c.After.Delegates)
				if !slices.Equal(before, []string{lagoon.ID}) || !slices.Equal(after, botDelegates(t, s, poteto)) {
					t.Fatalf("change delegates before %q after %q, want [%s] then [%s %s]", before, after, lagoon.ID, lagoon.ID, mako.ID)
				}
			}
			var changes []struct {
				ID string `json:"id"`
			}
			s.do(t, "GET", "/api/threads/"+edit.thread+"/changes", nil, &changes, http.StatusOK)
			if len(changes) != 1 || changes[0].ID != change {
				t.Fatalf("GET /api/threads/%s/changes %+v, want only %s", edit.thread, changes, change)
			}

			s.do(t, "POST", "/api/platform/changes/"+change+"/undo", nil, nil, http.StatusNoContent)
			wantDelegates(t, s, poteto, "undo edit_bot delegates", lagoon.ID)
		})
	}
}

func TestDelegates_DeleteRemovesFromLists(t *testing.T) {
	_, s := start(t, "claude")
	mako := s.newBot(t, "Mako", "claude")
	lagoon := s.newBot(t, "Lagoon", "claude")
	poteto := s.newBot(t, "Poteto", "claude")
	s.do(t, "PUT", "/api/bots/"+lagoon.ID, map[string]any{"delegates": []string{mako.ID}}, nil, http.StatusOK)
	s.do(t, "PUT", "/api/bots/"+poteto.ID, map[string]any{"delegates": []string{mako.ID, lagoon.ID}}, nil, http.StatusOK)
	wantDelegates(t, s, lagoon.ID, "before delete", mako.ID)
	wantDelegates(t, s, poteto.ID, "before delete", mako.ID, lagoon.ID)

	s.do(t, "DELETE", "/api/bots/"+mako.ID, nil, nil, http.StatusNoContent)

	wantDelegates(t, s, lagoon.ID, "after deleting Mako")
	wantDelegates(t, s, poteto.ID, "after deleting Mako", lagoon.ID)
}
