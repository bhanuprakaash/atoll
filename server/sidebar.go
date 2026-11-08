package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"atoll/run"
	"atoll/store"
)

// sidebarThread is an open thread of a Bot with the one marker the sidebar shows. Helper is set
// for a thread another Bot handed to this one: the user cannot close it.
type sidebarThread struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"` // "needs_you", "running", "new" or ""
	Pinned bool   `json:"pinned"`
	Helper bool   `json:"helper,omitempty"`
}

// notice is one /api/events payload: an event of a thread with the thread's Bot and title.
type notice struct {
	ThreadID string `json:"thread_id"`
	BotID    string `json:"bot_id"`
	BotName  string `json:"bot_name"`
	Title    string `json:"title"`
	Data     any    `json:"data"`
}

// sidebarThreads keeps the open threads in the order given and marks each: needs_you when its run
// or a helper's waits on an approval, running when its run or a helper's goes, new when its newest
// run finished after the user last opened it.
func (a *api) sidebarThreads(threads, helpers []store.ThreadRun) []sidebarThread {
	byBoss := map[string][]store.ThreadRun{}
	for _, h := range helpers {
		byBoss[h.ParentThreadID] = append(byBoss[h.ParentThreadID], h)
	}
	out := []sidebarThread{}
	for _, t := range threads {
		if t.Closed {
			continue
		}
		state := a.marker(t, byBoss[t.ThreadID])
		out = append(out, sidebarThread{
			ID: t.ThreadID, Title: t.Title, State: state, Pinned: t.Pinned, Helper: t.ParentThreadID != "",
		})
	}
	return out
}

// marker is the state of thread t with its helpers.
func (a *api) marker(t store.ThreadRun, helpers []store.ThreadRun) string {
	state := ""
	waiting := func(tr store.ThreadRun) bool {
		if tr.Status != store.StatusRunning && tr.Status != store.StatusWaiting {
			return false
		}
		state = "running"
		_, approvals, _ := a.runs.Active(tr.ThreadID)
		return len(approvals) > 0
	}
	for _, h := range helpers {
		if waiting(h) {
			return "needs_you"
		}
	}
	if waiting(t) {
		return "needs_you"
	}
	if state == "" && t.Finished.After(t.Opened) {
		return "new"
	}
	return state
}

func (a *api) markOpened(w http.ResponseWriter, r *http.Request) {
	if err := a.st.MarkOpened(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) pinThread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Replace string `json:"replace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := a.st.PinThread(r.Context(), r.PathValue("id"), body.Replace); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) unpinThread(w http.ResponseWriter, r *http.Request) {
	if err := a.st.UnpinThread(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) setBotGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GroupID *string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id := r.PathValue("id")
	if id == _chiefID {
		fail(w, http.StatusConflict, "Chief is always first")
		return
	}
	groupID := ""
	if body.GroupID != nil {
		groupID = *body.GroupID
	}
	if err := a.st.SetGroup(r.Context(), id, groupID); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := a.st.Groups(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, orEmpty(groups))
}

func (a *api) createGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.Name) == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	g, err := a.st.CreateGroup(r.Context(), strings.TrimSpace(body.Name))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, g)
}

func (a *api) patchGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   *string `json:"name"`
		Folded *bool   `json:"folded"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || (body.Name == nil && body.Folded == nil) ||
		(body.Name != nil && strings.TrimSpace(*body.Name) == "") {
		fail(w, http.StatusBadRequest, "name or folded is required")
		return
	}
	id := r.PathValue("id")
	if body.Name != nil {
		if err := a.st.SetGroupName(r.Context(), id, strings.TrimSpace(*body.Name)); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if body.Folded != nil {
		if err := a.st.SetGroupFolded(r.Context(), id, *body.Folded); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) moveGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Up *bool `json:"up"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Up == nil {
		fail(w, http.StatusBadRequest, "up is required")
		return
	}
	if err := a.st.MoveGroup(r.Context(), r.PathValue("id"), *body.Up); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteGroup(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// notices streams the events of every thread that the sidebar and the island show, each with its
// thread's Bot and title. An event whose thread is gone is skipped.
func (a *api) notices(w http.ResponseWriter, r *http.Request) {
	events, cancel := a.runs.SubscribeAll()
	defer cancel()
	a.stream(w, r, events, func(ev run.Event) (any, bool) {
		t, err := a.st.Thread(r.Context(), ev.ThreadID)
		if err != nil {
			return nil, false
		}
		b, err := a.st.Bot(r.Context(), t.BotID)
		if err != nil {
			return nil, false
		}
		return notice{ThreadID: t.ID, BotID: t.BotID, BotName: b.Name, Title: t.Title, Data: ev.Data}, true
	})
}
