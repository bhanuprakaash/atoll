package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"atoll/connection"
	"atoll/run"
	"atoll/store"
)

type settings struct {
	WorktreeRoot string `json:"worktree_root"`
	UserName     string `json:"user_name"`
	DataDir      string `json:"data_dir"`
}

func (a *api) getSettings(w http.ResponseWriter, r *http.Request) {
	root, err := a.runs.WorktreeRoot(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	name, err := a.st.Setting(r.Context(), "user_name")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, settings{WorktreeRoot: root, UserName: name, DataDir: a.runs.DataDir()})
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
	if err := a.st.SetSetting(r.Context(), "user_name", body.UserName); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) pickRepoFolder(w http.ResponseWriter, r *http.Request) {
	name, args := "osascript", []string{"-e", `POSIX path of (choose folder with prompt "Choose a repo folder")`}
	if runtime.GOOS == "linux" {
		name, args = "zenity", []string{"--file-selection", "--directory", "--title", "Choose a repo folder"}
	}
	out, err := exec.CommandContext(r.Context(), name, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.Is(err, exec.ErrNotFound):
			fail(w, http.StatusNotImplemented, "No folder picker on this system.")
		case !errors.As(err, &exitErr):
			a.fail(w, r, err)
		case runtime.GOOS == "linux" && exitErr.ExitCode() == 1,
			strings.Contains(string(exitErr.Stderr), "User canceled") && strings.Contains(string(exitErr.Stderr), "-128"):
			w.WriteHeader(http.StatusNoContent)
		default:
			a.fail(w, r, err)
		}
		return
	}
	path := strings.TrimSuffix(strings.TrimRight(string(out), "\r\n"), "/")
	if path == "" {
		path = "/"
	}
	write(w, http.StatusOK, struct {
		Path string `json:"path"`
	}{Path: path})
}

func (a *api) createRepo(w http.ResponseWriter, r *http.Request) {
	var body store.Repo
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		fail(w, http.StatusBadRequest, "name and path are required")
		return
	}
	if err := validateRepo(r.Context(), body); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	rp, err := a.st.CreateRepo(r.Context(), body.Name, body.Path)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rp.Host = run.RepoHost(r.Context(), rp.Path)
	write(w, http.StatusCreated, rp)
}

func (a *api) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := a.st.Repos(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for i := range repos {
		repos[i].Host = run.RepoHost(r.Context(), repos[i].Path)
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
		InstanceURL  string `json:"instance_url"`
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var existing store.Connection
	var err error
	if body.ConnectionID != "" {
		existing, err = a.st.Connection(r.Context(), body.ConnectionID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if existing.Kind != store.KindGitLab {
			fail(w, http.StatusBadRequest, "connection is not GitLab")
			return
		}
		body.InstanceURL = existing.InstanceURL
	}
	if body.InstanceURL == "" {
		body.InstanceURL = connection.DefaultInstance
	}
	dev, err := connection.StartDevice(r.Context(), a.http, body.InstanceURL)
	if err != nil {
		a.failSignIn(w, "start GitLab sign-in", body.InstanceURL, err)
		return
	}
	a.writeDevice(w, r, store.KindGitLab, body.InstanceURL, existing, dev)
}

// startGitHub takes an optional connection_id to re-sign-in an existing connection.
func (a *api) startGitHub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var existing store.Connection
	if body.ConnectionID != "" {
		var err error
		existing, err = a.st.Connection(r.Context(), body.ConnectionID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if existing.Kind != store.KindGitHub {
			fail(w, http.StatusBadRequest, "connection is not GitHub")
			return
		}
	}
	dev, err := connection.StartGitHubDevice(r.Context(), a.http)
	if err != nil {
		a.failSignIn(w, "start GitHub sign-in", connection.GitHubURL(), err)
		return
	}
	a.writeDevice(w, r, store.KindGitHub, connection.GitHubInstance, existing, dev)
}

func (a *api) writeDevice(
	w http.ResponseWriter, r *http.Request, kind, instance string, existing store.Connection, dev connection.Device,
) {
	c := existing
	if existing.ID == "" {
		var err error
		c, err = a.st.CreateConnection(r.Context(), kind, instance)
		if err != nil {
			a.fail(w, r, err)
			return
		}
	} else if err := a.st.SetConnectionStatus(r.Context(), c.ID, store.ConnectionPending); err != nil {
		a.fail(w, r, err)
		return
	}
	a.mu.Lock()
	interval := dev.Interval
	if interval <= 0 {
		interval = _defaultDeviceInterval
	}
	a.devices[c.ID] = &device{code: dev.DeviceCode, interval: interval}
	a.mu.Unlock()
	write(w, http.StatusCreated, struct {
		ID              string `json:"id"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		IntervalMs      int64  `json:"interval_ms"`
	}{c.ID, dev.UserCode, dev.VerificationURI, dev.Interval.Milliseconds()})
}

// pollConnection stores an approved token only in the OS secret store.
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
	dev, ok := a.devices[c.ID]
	early := ok && time.Now().Before(dev.next)
	if ok && !early {
		dev.next = time.Now().Add(dev.interval)
	}
	a.mu.Unlock()
	if !ok {
		fail(w, http.StatusConflict, "this sign-in is not in progress: start it again")
		return
	}
	if early {
		writeStatus(w, store.ConnectionPending)
		return
	}
	var save func() error
	if c.Kind == store.KindGitHub {
		var token string
		token, err = connection.PollGitHubToken(r.Context(), a.http, dev.code)
		save = func() error { return connection.SaveGitHubToken(c.ID, token) }
	} else {
		var tokens connection.Tokens
		tokens, err = connection.PollToken(r.Context(), a.http, c.InstanceURL, dev.code)
		save = func() error { return connection.SaveTokens(c.ID, tokens) }
	}
	if errors.Is(err, connection.ErrSlowDown) {
		a.mu.Lock()
		dev.interval += 5 * time.Second
		dev.next = time.Now().Add(dev.interval)
		a.mu.Unlock()
	}
	if errors.Is(err, connection.ErrPending) {
		writeStatus(w, store.ConnectionPending)
		return
	}
	if err != nil {
		instance := c.InstanceURL
		if c.Kind == store.KindGitHub {
			instance = connection.GitHubURL()
		}
		a.failSignIn(w, "poll sign-in "+c.ID, instance, err)
		return
	}
	if err := save(); err != nil {
		a.log.Error("save sign-in", "connection_id", c.ID, "kind", c.Kind, "error", err)
		a.mu.Lock()
		delete(a.devices, c.ID)
		a.mu.Unlock()
		if err := a.st.SetConnectionStatus(r.Context(), c.ID, store.ConnectionNeedsSignIn); err != nil {
			a.fail(w, r, err)
			return
		}
		name := "GitLab"
		if c.Kind == store.KindGitHub {
			name = "GitHub"
		}
		failKeychain(w, "the "+name+" sign-in")
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

// failSignIn answers a sign-in call that failed: a provider that says the sign-in ended is
// signed_out, any other failure is the instance not answering.
func (a *api) failSignIn(w http.ResponseWriter, what, instance string, err error) {
	a.log.Warn(what, "error", err)
	if errors.Is(err, connection.ErrSignInEnded) {
		failCode(w, http.StatusUnauthorized, "signed_out", "That sign-in expired or was refused.")
		return
	}
	host := instance
	if u, perr := url.Parse(instance); perr == nil && u.Host != "" {
		host = u.Host
	}
	failCode(w, http.StatusBadGateway, "unreachable", "Reef couldn't reach "+host+".")
}

func writeStatus(w http.ResponseWriter, status string) {
	write(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: status})
}
