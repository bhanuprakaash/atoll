package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"atoll/store"
)

// sidebarThread is an open thread of a Bot with the one marker the sidebar shows. Helper is set
// for a thread another Bot handed to this one: the user cannot close it. ParentThreadID is its
// boss thread, Step what a helper's run does now, Wake why a PR wake started it, and Activity the live verb or the waiting ask.
type sidebarThread struct {
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	State          string      `json:"state"` // "needs_you", "running", "failed", "limited", "new" or ""
	Pinned         bool        `json:"pinned"`
	Helper         bool        `json:"helper,omitempty"`
	ParentThreadID string      `json:"parent_thread_id"`
	Step           string      `json:"step,omitempty"`
	Wake           string      `json:"wake,omitempty"`
	Activity       string      `json:"activity,omitempty"`
	Hidden         bool        `json:"hidden,omitempty"` // PR merged: open and in the popup, not in the tree
	Branch         string      `json:"branch"`
	PRs            []prSummary `json:"prs"`
	LastActivity   time.Time   `json:"last_activity"`
	// ResetsAt is when the provider's usage limit ends, for a limited thread.
	ResetsAt string `json:"resets_at,omitempty"`
}

// sidebarThreads keeps the open threads in the order given and marks each, the first that holds:
// needs_you when its run waits on an approval, running when its run or a helper's goes, failed when
// a run failed after the user last opened it, limited while the provider's usage limit lasts, new
// when it replied after the user last opened it.
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
		resetsAt := ""
		if state == "limited" {
			resetsAt = t.LimitedUntil.Format(time.RFC3339)
		}
		step, wake := "", ""
		activity := ""
		if state == "running" || state == "needs_you" {
			activity = a.liveVerb(t.ThreadID)
			if _, approvals, _ := a.runs.Active(t.ThreadID); len(approvals) > 0 {
				activity = needsYou(approvals[0])
			}
			if t.ParentThreadID != "" {
				step, wake = a.liveStep(t.ThreadID), a.runs.ActiveWake(t.ThreadID)
			}
		}
		out = append(out, sidebarThread{
			ID: t.ThreadID, ParentThreadID: t.ParentThreadID, Step: step, Wake: wake, Activity: activity, Title: t.Title, State: state, Pinned: t.Pinned, Helper: t.ParentThreadID != "",
			Branch: t.Branch, PRs: prSummaries(t.PRs), LastActivity: t.LastActivity, ResetsAt: resetsAt,
			Hidden: t.Merged && !t.Pinned && (state == "" || state == "new"),
		})
	}
	return out
}

// marker is the state of thread t. A helper's approval is not t's: it shows on the line under t.
func (a *api) marker(t store.ThreadRun, helpers []store.ThreadRun) string {
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
		if h.Status == store.StatusRunning || h.Status == store.StatusWaiting {
			state = "running"
		}
	}
	if needs(t) {
		return "needs_you"
	}
	switch {
	case state != "":
		return state
	case t.Failed:
		return "failed"
	case t.LimitedUntil.After(time.Now()):
		return "limited"
	case t.Unseen && t.ParentThreadID == "":
		return "new"
	}
	return ""
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

func (a *api) placeBot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pinned  bool   `json:"pinned"`
		GroupID string `json:"group_id"`
		Before  string `json:"before"`
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
	if err := a.st.PlaceBot(r.Context(), id, body.Pinned, body.GroupID, body.Before); err != nil {
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
		Before string `json:"before"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := a.st.MoveGroup(r.Context(), r.PathValue("id"), body.Before); err != nil {
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
