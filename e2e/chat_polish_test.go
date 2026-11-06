package e2e

import (
	"net/http"
	"slices"
	"testing"
)

func TestChatPolish_AutoTitleAndUserTitle(t *testing.T) {
	_, s := start(t, "claude")
	b := s.newBot(t, "Title", "claude")
	th := s.newThread(t, b)

	first := "One\t two three four five six!!! seven"
	ev := s.events(t, th)
	defer until(t, ev, s.send(t, th, first).ID, "run_finished")
	if got, want := s.thread(t, th.ID).Title, "One two three four five six"; got != want {
		t.Errorf("first-message title %q, want %q", got, want)
	}

	user := s.newThread(t, b)
	var edited threadInfo
	s.do(t, "PATCH", "/api/threads/"+user.ID, map[string]string{"title": "Planning notes"}, &edited, http.StatusOK)
	if edited.Title != "Planning notes" {
		t.Errorf("PATCH title %q, want %q", edited.Title, "Planning notes")
	}
	userEv := s.events(t, user)
	defer until(t, userEv, s.send(t, user, "One two three four five six.").ID, "run_finished")
	if got := s.thread(t, user.ID).Title; got != "Planning notes" {
		t.Errorf("user title after first message %q, want %q", got, "Planning notes")
	}
}

func TestChatPolish_SidebarActivity(t *testing.T) {
	_, s := start(t, "claude")
	b := s.newBot(t, "Reply", "claude")
	th := s.newThread(t, b)
	ev := s.events(t, th)
	s.finished(t, ev, s.send(t, th, _keepPrompt).ID)

	if got, want := s.bot(t, b.ID).Activity, "ok"; got != want {
		t.Errorf("Bot activity after reply %q, want latest reply's first line %q", got, want)
	}
}

func TestChatPolish_PinBotsPersistAndOrder(t *testing.T) {
	m, s := start(t, "claude")
	first := s.newBot(t, "First", "claude")
	second := s.newBot(t, "Second", "claude")
	s.do(t, "POST", "/api/bots/"+second.ID+"/pin", nil, nil, http.StatusNoContent)
	s.do(t, "POST", "/api/bots/"+first.ID+"/pin", nil, nil, http.StatusNoContent)
	s.do(t, "POST", "/api/bots/chief/pin", nil, nil, http.StatusConflict)

	var bots []botInfo
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	var got []string
	for _, b := range bots {
		got = append(got, b.Name)
	}
	if want := []string{"Chief", "Second", "First"}; len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Errorf("Bot order %q, want prefix %q", got, want)
	}
	if !s.bot(t, first.ID).Pinned || !s.bot(t, second.ID).Pinned {
		t.Errorf("pinned Bots must return pinned:true")
	}

	s.kill()
	s = m.serve(t)
	if !s.bot(t, first.ID).Pinned || !s.bot(t, second.ID).Pinned {
		t.Errorf("pinned Bots lost pinned:true after restart")
	}
	s.do(t, "DELETE", "/api/bots/"+first.ID+"/pin", nil, nil, http.StatusNoContent)
}

func TestChatPolish_OlderMessages(t *testing.T) {
	_, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "History", "claude"))
	ev := s.events(t, th)
	for range 26 {
		s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
	}

	var latest []message
	s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &latest, http.StatusOK)
	if len(latest) != 50 {
		t.Fatalf("latest messages %d, want 50", len(latest))
	}
	if latest[0].Role != "user" || latest[len(latest)-1].Role != "bot" {
		t.Errorf("latest messages are not in chronological order: first=%+v last=%+v", latest[0], latest[len(latest)-1])
	}

	var older []message
	s.do(t, "GET", "/api/threads/"+th.ID+"/messages?before="+latest[0].ID, nil, &older, http.StatusOK)
	if len(older) != 2 || older[0].Role != "user" || older[1].Role != "bot" {
		t.Errorf("older messages %+v, want the two chronological messages before %s", older, latest[0].ID)
	}
}
