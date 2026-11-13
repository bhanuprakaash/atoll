package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"atoll/run"
	"atoll/store"
)

// sidebarThread is an open thread of a Bot with the one marker the sidebar shows. Helper is set
// for a thread another Bot handed to this one: the user cannot close it.
type sidebarThread struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	State        string    `json:"state"` // "needs_you", "waiting", "running", "new" or ""
	Pinned       bool      `json:"pinned"`
	Helper       bool      `json:"helper,omitempty"`
	Branch       string    `json:"branch"`
	MRURL        string    `json:"mr_url"`
	LastActivity time.Time `json:"last_activity"`
	// Waiting is what waits on the person in the thread.
	Waiting []store.Waiting `json:"waiting,omitempty"`
}

// notice is one /api/events payload: an event of a thread with the thread's Bot and title.
type notice struct {
	ThreadID       string `json:"thread_id"`
	ParentThreadID string `json:"parent_thread_id"`
	BotID          string `json:"bot_id"`
	BotName        string `json:"bot_name"`
	Title          string `json:"title"`
	Data           any    `json:"data"`
}

// sidebarThreads keeps the open threads in the order given and marks each: needs_you when its run
// or a helper's waits on an approval, waiting when something else waits on the person, running when
// its run or a helper's goes, new when its newest run finished after the user last opened it.
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
		state := a.marker(t, byBoss[t.ThreadID], len(t.Waiting) > 0)
		out = append(out, sidebarThread{
			ID: t.ThreadID, Title: t.Title, State: state, Pinned: t.Pinned, Helper: t.ParentThreadID != "",
			Branch: t.Branch, MRURL: t.MRURL, LastActivity: t.LastActivity,
			Waiting: t.Waiting,
		})
	}
	return out
}

// marker is the state of thread t with its helpers.
func (a *api) marker(t store.ThreadRun, helpers []store.ThreadRun, waits bool) string {
	state := ""
	needs := func(tr store.ThreadRun) bool {
		if tr.Status != store.StatusRunning && tr.Status != store.StatusWaiting {
			return false
		}
		state = "running"
		_, approvals, _ := a.runs.Active(tr.ThreadID)
		return len(approvals) > 0
	}
	for _, h := range helpers {
		if needs(h) {
			return "needs_you"
		}
	}
	if needs(t) {
		return "needs_you"
	}
	if waits {
		return "waiting"
	}
	if state == "" && t.Unseen {
		return "new"
	}
	return state
}

func (a *api) markOpened(w http.ResponseWriter, r *http.Request) {
	if err := a.st.MarkOpened(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	a.runs.ThreadRead(r.PathValue("id"))
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

// earlierNotice is one row of the island's Earlier list: a notice of the type and with the data a
// live event of that type has, and when it happened.
type earlierNotice struct {
	notice
	Type string    `json:"type"`
	At   time.Time `json:"at"`
	key  string
}

// notices streams the events of every thread that the sidebar and the island show, each with its
// thread's Bot and title: the approvals open now, then the island's Earlier list, then what
// happens. A live event that Earlier already lists is skipped, as is an event whose thread is gone.
func (a *api) notices(w http.ResponseWriter, r *http.Request) {
	open, events, cancel := a.runs.SubscribeAll()
	defer cancel()
	earlier, err := a.earlier(r.Context())
	if err != nil {
		a.log.Error("build earlier", "err", err)
	}
	lead := make([]sseEvent, 0, len(open)+1)
	for _, ev := range open {
		if n, err := a.noticeOf(r.Context(), ev); err == nil {
			lead = append(lead, sseEvent{Type: ev.Type, Data: n})
		}
	}
	lead = append(lead, sseEvent{Type: "earlier", Data: earlier})
	listed := make(map[string]bool, len(earlier))
	for _, n := range earlier {
		listed[n.key] = true
	}
	a.stream(w, r, events, func(ev run.Event) (any, bool) {
		if key := eventKey(ev.Data); listed[key] {
			delete(listed, key)
			return nil, false
		}
		n, err := a.noticeOf(r.Context(), ev)
		return n, err == nil
	}, lead...)
}

// eventKey names what a run, MR or platform event is about, so that Earlier and the live stream
// never both show it. Other events have no key.
func eventKey(data any) string {
	switch d := data.(type) {
	case run.Finished:
		return "run " + d.RunID
	case run.MRUpdated:
		return "mr " + d.URL + " " + d.State
	case run.PlatformChanged:
		return "change " + d.Change.ID
	}
	return ""
}

// earlier lists, newest first, what happened after the island was last read: runs that ended, MRs
// opened and platform changes.
func (a *api) earlier(ctx context.Context) ([]earlierNotice, error) {
	out := []earlierNotice{}
	since, err := a.st.IslandSeenAt(ctx)
	if err != nil {
		return out, err
	}
	add := func(typ string, at time.Time, threadID string, data any) {
		if n, err := a.noticeAbout(ctx, threadID, data); err == nil {
			out = append(out, earlierNotice{notice: n, Type: typ, At: at, key: eventKey(data)})
		}
	}
	runs, err := a.st.FinishedRunsSince(ctx, since)
	if err != nil {
		return out, err
	}
	for _, e := range runs {
		add(run.EventRunFinished, e.At, e.ThreadID, run.Finished{RunID: e.RunID, Status: e.Status, Error: e.Error})
	}
	mrs, err := a.st.OpenedMRsSince(ctx, since)
	if err != nil {
		return out, err
	}
	for _, e := range mrs {
		add(run.EventMRUpdated, e.At, e.ThreadID, run.MRUpdated{URL: e.URL, State: "opened", Title: e.Title})
	}
	changes, err := a.st.ChangesSince(ctx, since)
	if err != nil {
		return out, err
	}
	for _, c := range changes {
		add(run.EventPlatformChanged, c.Created, c.SourceThreadID,
			run.PlatformChanged{SourceBotID: c.SourceBotID, ThreadID: c.SourceThreadID, RunID: c.SourceRunID, Change: c})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

func (a *api) markIslandSeen(w http.ResponseWriter, r *http.Request) {
	if err := a.st.MarkIslandSeen(context.WithoutCancel(r.Context())); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// dismissIsland clears one Earlier row: a run row marks its thread read, an MR row and a platform
// change row are hidden.
func (a *api) dismissIsland(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.ID == "" {
		fail(w, http.StatusBadRequest, "type and id are required")
		return
	}
	var err error
	switch body.Type {
	case run.EventRunFinished:
		if err = a.st.MarkOpened(r.Context(), body.ID); err == nil {
			a.runs.ThreadRead(body.ID)
		}
	case run.EventMRUpdated:
		err = a.st.DismissMR(r.Context(), body.ID)
	case run.EventPlatformChanged:
		err = a.st.DismissChange(r.Context(), body.ID)
	default:
		fail(w, http.StatusBadRequest, "unknown type")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) noticeOf(ctx context.Context, ev run.Event) (notice, error) {
	return a.noticeAbout(ctx, ev.ThreadID, ev.Data)
}

func (a *api) noticeAbout(ctx context.Context, threadID string, data any) (notice, error) {
	t, err := a.st.Thread(ctx, threadID)
	if err != nil {
		return notice{}, err
	}
	b, err := a.st.Bot(ctx, t.BotID)
	if err != nil {
		return notice{}, err
	}
	return notice{
		ThreadID: t.ID, ParentThreadID: t.ParentThreadID, BotID: t.BotID, BotName: b.Name,
		Title: t.Title, Data: data,
	}, nil
}
