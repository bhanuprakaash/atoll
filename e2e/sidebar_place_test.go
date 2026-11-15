package e2e

import (
	"net/http"
	"slices"
	"testing"
)

// Drag to reorder on the rail (#246): PUT /api/bots/{id}/place {pinned, group_id, before} moves a
// Bot to a place in Pinned, a group or Ungrouped, POST /api/groups/{id}/move {before} moves a group,
// and an upgrade keeps the order an install already had. Contract: e2e/sidebar-rail-contract.md.

type placedBot struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Pinned  bool   `json:"pinned"`
	GroupID string `json:"group_id"`
}

type railGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// railSection is the names of the Bots in one rail section, in the order GET /api/bots gives them:
// pinned Bots, or the unpinned Bots of group (empty for Ungrouped). Chief is in none.
func railSection(t *testing.T, s *server, pinned bool, group string) []string {
	t.Helper()
	var bots []placedBot
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	var names []string
	for _, b := range bots {
		if b.ID == "chief" {
			continue
		}
		if pinned && b.Pinned || !pinned && !b.Pinned && b.GroupID == group {
			names = append(names, b.Name)
		}
	}
	return names
}

func groupOrder(t *testing.T, s *server) []string {
	t.Helper()
	var groups []railGroup
	s.do(t, "GET", "/api/groups", nil, &groups, http.StatusOK)
	var names []string
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}

func newGroup(t *testing.T, s *server, name string) string {
	t.Helper()
	var g railGroup
	s.do(t, "POST", "/api/groups", map[string]string{"name": name}, &g, http.StatusCreated)
	return g.ID
}

func place(t *testing.T, s *server, id string, pinned bool, group, before any) {
	t.Helper()
	s.do(t, "PUT", "/api/bots/"+id+"/place", map[string]any{
		"pinned": pinned, "group_id": group, "before": before,
	}, nil, http.StatusNoContent)
}

func wantSection(t *testing.T, s *server, when string, pinned bool, group string, want ...string) {
	t.Helper()
	if got := railSection(t, s, pinned, group); !slices.Equal(got, want) {
		t.Errorf("%s: section (pinned %v, group %q) is %q, want %q", when, pinned, group, got, want)
	}
}

func TestPlace_MovesBotsWithinAndAcrossSections(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	ids := map[string]string{}
	for _, name := range []string{"Ash", "Birch", "Cedar", "Elm"} {
		ids[name] = s.newBot(t, name, "claude").ID
	}
	g := newGroup(t, s, "Platform")
	for _, name := range []string{"Ash", "Birch", "Cedar"} {
		s.do(t, "PUT", "/api/bots/"+ids[name]+"/group", map[string]any{"group_id": g}, nil, http.StatusNoContent)
	}
	wantSection(t, s, "moved by the menu, each goes last", false, g, "Ash", "Birch", "Cedar")

	place(t, s, ids["Elm"], false, g, ids["Birch"])
	wantSection(t, s, "Elm dropped before Birch", false, g, "Ash", "Elm", "Birch", "Cedar")
	wantSection(t, s, "Elm left Ungrouped", false, "")

	place(t, s, ids["Ash"], false, g, nil)
	wantSection(t, s, "Ash dropped last", false, g, "Elm", "Birch", "Cedar", "Ash")

	place(t, s, ids["Birch"], true, nil, nil)
	place(t, s, ids["Cedar"], true, nil, ids["Birch"])
	wantSection(t, s, "Birch, then Cedar above it, dropped into Pinned", true, "", "Cedar", "Birch")
	wantSection(t, s, "after pinning two", false, g, "Elm", "Ash")

	place(t, s, ids["Birch"], false, g, ids["Elm"])
	wantSection(t, s, "Birch dragged out of Pinned", true, "", "Cedar")
	wantSection(t, s, "Birch dragged out of Pinned", false, g, "Birch", "Elm", "Ash")
	var birch placedBot
	s.do(t, "GET", "/api/bots/"+ids["Birch"], nil, &birch, http.StatusOK)
	if birch.Pinned || birch.GroupID != g {
		t.Errorf("Birch after the drag out of Pinned: pinned %v, group %q; want unpinned in %q", birch.Pinned, birch.GroupID, g)
	}

	fresh := s.newBot(t, "Fir", "claude")
	s.do(t, "POST", "/api/bots/"+fresh.ID+"/pin", nil, nil, http.StatusNoContent)
	wantSection(t, s, "a newly pinned Bot goes last", true, "", "Cedar", "Fir")

	s.do(t, "PUT", "/api/bots/chief/place", map[string]any{"pinned": true, "group_id": nil, "before": nil}, nil, http.StatusConflict)

	s.kill()
	s = m.serve(t)
	wantSection(t, s, "after a restart", true, "", "Cedar", "Fir")
	wantSection(t, s, "after a restart", false, g, "Birch", "Elm", "Ash")
}

func TestGroupMove_Before(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	ids := map[string]string{}
	for _, name := range []string{"Platform", "Frontend", "Data"} {
		ids[name] = newGroup(t, s, name)
	}
	move := func(name string, before any) {
		t.Helper()
		s.do(t, "POST", "/api/groups/"+ids[name]+"/move", map[string]any{"before": before}, nil, http.StatusNoContent)
	}
	move("Frontend", ids["Platform"])
	if got, want := groupOrder(t, s), []string{"Frontend", "Platform", "Data"}; !slices.Equal(got, want) {
		t.Errorf("Frontend moved before Platform: groups %q, want %q", got, want)
	}
	move("Platform", nil)
	want := []string{"Frontend", "Data", "Platform"}
	if got := groupOrder(t, s); !slices.Equal(got, want) {
		t.Errorf("Platform moved last: groups %q, want %q", got, want)
	}
	s.kill()
	s = m.serve(t)
	if got := groupOrder(t, s); !slices.Equal(got, want) {
		t.Errorf("after a restart: groups %q, want %q", got, want)
	}
}

// testdata/before-place.sql is an install from before Bots had a position: Fir then Ash pinned (Ash
// is the older Bot), Ivy and Oak in Backend (Oak moved there first, Ivy is older), Yew in Frontend,
// Elm ungrouped.
func TestPlace_UpgradeKeepsTheOrder(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	dbExec(t, m, mustRead(t, "testdata/before-place.sql"))
	s := m.serve(t)

	var groups []railGroup
	s.do(t, "GET", "/api/groups", nil, &groups, http.StatusOK)
	byName := map[string]string{}
	for _, g := range groups {
		byName[g.Name] = g.ID
	}
	check := func(when string) {
		t.Helper()
		if got, want := groupOrder(t, s), []string{"Backend", "Frontend"}; !slices.Equal(got, want) {
			t.Errorf("%s: groups %q, want %q", when, got, want)
		}
		wantSection(t, s, when, true, "", "Fir", "Ash")
		wantSection(t, s, when, false, byName["Backend"], "Ivy", "Oak")
		wantSection(t, s, when, false, byName["Frontend"], "Yew")
		wantSection(t, s, when, false, "", "Elm")
	}
	check("after the upgrade")
	s.kill()
	s = m.serve(t)
	check("after a restart")

	fresh := s.newBot(t, "Pine", "claude")
	s.do(t, "PUT", "/api/bots/"+fresh.ID+"/group", map[string]any{"group_id": byName["Backend"]}, nil, http.StatusNoContent)
	wantSection(t, s, "a Bot moved by the menu after the upgrade", false, byName["Backend"], "Ivy", "Oak", "Pine")
}
