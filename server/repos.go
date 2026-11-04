package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"

	"atoll/connection"
	"atoll/repo"
	"atoll/store"
)

type settings struct {
	WorktreeRoot string `json:"worktree_root"`
}

func (a *api) getSettings(w http.ResponseWriter, r *http.Request) {
	root, err := a.runs.WorktreeRoot(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, settings{WorktreeRoot: root})
}

func (a *api) putSettings(w http.ResponseWriter, r *http.Request) {
	var body settings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !filepath.IsAbs(body.WorktreeRoot) {
		fail(w, http.StatusBadRequest, "worktree_root must be an absolute path")
		return
	}
	if err := a.runs.SetWorktreeRoot(r.Context(), body.WorktreeRoot); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) createRepo(w http.ResponseWriter, r *http.Request) {
	var body store.Repo
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		fail(w, http.StatusBadRequest, "name and path are required")
		return
	}
	if !filepath.IsAbs(body.Path) || !repo.IsRepo(r.Context(), body.Path) {
		fail(w, http.StatusBadRequest, "path must be the top level of a git repo with a commit")
		return
	}
	rp, err := a.st.CreateRepo(r.Context(), body.Name, body.Path)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, rp)
}

func (a *api) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := a.st.Repos(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, orEmpty(repos))
}

func (a *api) closeThread(w http.ResponseWriter, r *http.Request) {
	if err := a.runs.CloseThread(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := a.st.Connections(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, orEmpty(conns))
}

func (a *api) getConnection(w http.ResponseWriter, r *http.Request) {
	c, err := a.st.Connection(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, c)
}

// startGitLab starts a device-flow sign-in. The device code stays in memory: a sign-in that is
// pending when Atoll stops has to be started again.
func (a *api) startGitLab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceURL string `json:"instance_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.InstanceURL == "" {
		body.InstanceURL = connection.DefaultInstance
	}
	dev, err := connection.StartDevice(r.Context(), a.http, body.InstanceURL)
	if err != nil {
		a.log.Warn("start GitLab sign-in", "error", err)
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	c, err := a.st.CreateConnection(r.Context(), store.KindGitLab, body.InstanceURL)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.mu.Lock()
	a.devices[c.ID] = dev.DeviceCode
	a.mu.Unlock()
	write(w, http.StatusCreated, struct {
		ID              string `json:"id"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		IntervalMs      int64  `json:"interval_ms"`
	}{c.ID, dev.UserCode, dev.VerificationURI, dev.Interval.Milliseconds()})
}

// pollConnection asks GitLab once whether the user approved the sign-in. On approval the token
// goes to the OS secret store and no further.
func (a *api) pollConnection(w http.ResponseWriter, r *http.Request) {
	c, err := a.st.Connection(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if c.Status == store.ConnectionConnected {
		writeStatus(w, c.Status)
		return
	}
	a.mu.Lock()
	code, ok := a.devices[c.ID]
	a.mu.Unlock()
	if !ok {
		fail(w, http.StatusConflict, "this sign-in is not in progress: start it again")
		return
	}
	token, err := connection.PollToken(r.Context(), a.http, c.InstanceURL, code)
	if errors.Is(err, connection.ErrPending) {
		writeStatus(w, store.ConnectionPending)
		return
	}
	if err != nil {
		a.log.Warn("poll GitLab sign-in", "connection_id", c.ID, "error", err)
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := connection.SaveToken(c.ID, token); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.st.SetConnectionStatus(r.Context(), c.ID, store.ConnectionConnected); err != nil {
		a.fail(w, r, err)
		return
	}
	a.mu.Lock()
	delete(a.devices, c.ID)
	a.mu.Unlock()
	writeStatus(w, store.ConnectionConnected)
}

func writeStatus(w http.ResponseWriter, status string) {
	write(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: status})
}
