// Package server is Atoll's HTTP API: REST and a per-thread SSE stream under /api/, Atoll's MCP
// tools under /mcp/, and the UI at /.
package server

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"atoll/memory"
	"atoll/repo"
	"atoll/run"
	"atoll/store"
)

// MCPPath is where Atoll's MCP endpoint lives; a run's server is MCPPath + its token.
const MCPPath = "/mcp/"

const (
	_pingEvery     = 15 * time.Second
	_signInTimeout = 30 * time.Second
)

//go:embed all:ui
var _ui embed.FS

type api struct {
	st   *store.Store
	mem  *memory.Store
	runs *run.Manager
	log  *slog.Logger
	http *http.Client // for GitLab sign-in

	mu      sync.Mutex
	devices map[string]string // device code of each pending sign-in, by connection id
}

// New returns the handler for the API, the MCP tools and the UI. providers, when not nil, serves
// /api/providers.
func New(
	st *store.Store, mem *memory.Store, runs *run.Manager, log *slog.Logger, providers http.Handler,
) http.Handler {
	a := &api{
		st: st, mem: mem, runs: runs, log: log,
		http:    &http.Client{Timeout: _signInTimeout},
		devices: map[string]string{},
	}
	mux := http.NewServeMux()
	if providers != nil {
		mux.Handle("/api/providers", providers)
		mux.Handle("/api/providers/", providers)
	}
	mux.HandleFunc("GET /api/settings", a.getSettings)
	mux.HandleFunc("PUT /api/settings", a.putSettings)
	mux.HandleFunc("POST /api/repos", a.createRepo)
	mux.HandleFunc("POST /api/repos/pick-folder", a.pickRepoFolder)
	mux.HandleFunc("GET /api/repos", a.listRepos)
	mux.HandleFunc("POST /api/memory", a.createMemory)
	mux.HandleFunc("GET /api/memory", a.listMemory)
	mux.HandleFunc("POST /api/memory/{id}/retire", a.retireMemory)
	mux.HandleFunc("GET /api/connections", a.listConnections)
	mux.HandleFunc("POST /api/connections/gitlab/start", a.startGitLab)
	mux.HandleFunc("POST /api/connections/{id}/poll", a.pollConnection)
	mux.HandleFunc("GET /api/connections/{id}", a.getConnection)
	mux.HandleFunc("POST /api/bots", a.createBot)
	mux.HandleFunc("GET /api/bots", a.listBots)
	mux.HandleFunc("GET /api/bots/{id}", a.getBot)
	mux.HandleFunc("PUT /api/bots/{id}", a.putBot)
	mux.HandleFunc("POST /api/bots/{id}/pin", a.pinBot)
	mux.HandleFunc("DELETE /api/bots/{id}/pin", a.unpinBot)
	mux.HandleFunc("POST /api/skills", a.createSkill)
	mux.HandleFunc("GET /api/skills", a.listSkills)
	mux.HandleFunc("GET /api/skills/{id}", a.getSkill)
	mux.HandleFunc("PUT /api/skills/{id}", a.putSkill)
	mux.HandleFunc("POST /api/platform/changes/{id}/undo", a.undoChange)
	mux.HandleFunc("GET /api/threads/{id}/changes", a.listChanges)
	mux.HandleFunc("POST /api/bots/{id}/threads", a.createThread)
	mux.HandleFunc("GET /api/bots/{id}/threads", a.listThreads)
	mux.HandleFunc("GET /api/threads/{id}", a.getThread)
	mux.HandleFunc("PATCH /api/threads/{id}", a.patchThread)
	mux.HandleFunc("POST /api/threads/{id}/close", a.closeThread)
	mux.HandleFunc("POST /api/threads/{id}/messages", a.sendMessage)
	mux.HandleFunc("GET /api/threads/{id}/messages", a.listMessages)
	mux.HandleFunc("GET /api/threads/{id}/events", a.events)
	mux.HandleFunc("GET /api/runs/{id}", a.getRun)
	mux.HandleFunc("POST /api/runs/{id}/stop", a.stopRun)
	mux.HandleFunc("POST /api/approvals/{id}", a.answer)
	mux.HandleFunc(MCPPath+"{token}", a.mcp)
	// fs.Sub fails only for an invalid dir name.
	sub, _ := fs.Sub(_ui, "ui")
	mux.Handle("/", uiHandler(sub))
	return localOnly(mux)
}

// localOnly refuses an API or MCP request that is not for a local host, or that a page on
// another origin sent: otherwise any web page could drive Bots, which run code.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, MCPPath)
		if !guarded || (isLocalHost(r.Host) && sameOrigin(r)) {
			next.ServeHTTP(w, r)
			return
		}
		fail(w, http.StatusForbidden, "forbidden")
	})
}

func isLocalHost(host string) bool {
	u, err := url.Parse("//" + host)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func (a *api) createBot(w http.ResponseWriter, r *http.Request) {
	var b store.Bot
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !validBot(b) {
		fail(w, http.StatusBadRequest, "name is required and provider must be claude or codex")
		return
	}
	if b.Look == "" {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(_looks))))
		if err != nil {
			a.fail(w, r, err)
			return
		}
		b.Look = _looks[n.Int64()]
	}
	b, err := a.st.CreateBot(r.Context(), b)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, responseBot(b))
}

func (a *api) listBots(w http.ResponseWriter, r *http.Request) {
	bots, err := a.st.Bots(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	runs, err := a.st.ThreadRuns(r.Context(), "")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	threads := map[string][]store.ThreadRun{}
	for _, tr := range runs {
		threads[tr.BotID] = append(threads[tr.BotID], tr)
	}
	responses := make([]botResponse, 0, len(bots))
	for _, b := range bots {
		responses = append(responses, responseBot(a.botSummary(r, b, threads[b.ID])))
	}
	write(w, http.StatusOK, responses)
}

func (a *api) createThread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoID string `json:"repo_id"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	botID := r.PathValue("id")
	if body.RepoID != "" {
		bot, err := a.st.Bot(r.Context(), botID)
		var rp store.Repo
		if err == nil {
			rp, err = a.st.Repo(r.Context(), body.RepoID)
		}
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if !bot.RepoAccess.Allows(body.RepoID) {
			fail(w, http.StatusForbidden, "the Bot has no access to this repo")
			return
		}
		if !repo.HasCommit(r.Context(), rp.Path) {
			fail(w, http.StatusBadRequest, "repo has no commit yet")
			return
		}
	}
	t, err := a.st.CreateThread(r.Context(), botID, body.RepoID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if body.Text != "" {
		m, ru, err := a.runs.Send(r.Context(), t.ID, body.Text)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		summary, err := a.threadSummary(r, t)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		write(w, http.StatusCreated, struct {
			threadSummary
			Message store.Message `json:"message"`
			Run     store.Run     `json:"run"`
		}{summary, m, ru})
		return
	}
	write(w, http.StatusCreated, t)
}

type threadSummary struct {
	store.Thread
	RunID     string        `json:"run_id"`
	RunStatus string        `json:"run_status"`
	Approval  *run.Approval `json:"approval"`
	Verb      string        `json:"verb"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (a *api) threadSummary(r *http.Request, t store.Thread) (threadSummary, error) {
	t, err := a.st.Thread(r.Context(), t.ID)
	if err != nil {
		return threadSummary{}, err
	}
	out := threadSummary{Thread: t, UpdatedAt: t.LastActivity}
	out.Verb = a.liveVerb(t.ID)
	out.RunID, err = a.st.LatestRun(r.Context(), t.ID)
	if err != nil {
		return out, err
	}
	if out.RunID != "" {
		ru, err := a.st.Run(r.Context(), out.RunID)
		if err != nil {
			return out, err
		}
		out.RunStatus = ru.Status
	}
	_, approvals, _ := a.runs.Active(t.ID)
	if len(approvals) > 0 {
		out.Approval = &approvals[0]
	}
	return out, nil
}

// liveVerb is what the thread's active run is doing now, as the live run line shows it.
func (a *api) liveVerb(threadID string) string {
	tool := a.runs.ActiveTool(threadID)
	if tool.Verb == "" {
		return "Thinking"
	}
	return strings.TrimSpace(tool.Verb + " " + tool.Target)
}

func (a *api) listThreads(w http.ResponseWriter, r *http.Request) {
	threads, err := a.st.Threads(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	summaries := make([]threadSummary, 0, len(threads))
	for _, t := range threads {
		summary, err := a.threadSummary(r, t)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		summaries = append(summaries, summary)
	}
	write(w, http.StatusOK, summaries)
}

func (a *api) getThread(w http.ResponseWriter, r *http.Request) {
	t, err := a.st.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var active *store.Run
	runID, approvals, ok := a.runs.Active(t.ID)
	if ok {
		ru, err := a.st.Run(r.Context(), runID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		active = &ru
	}
	summary, err := a.threadSummary(r, t)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, struct {
		threadSummary
		ActiveRun *store.Run     `json:"active_run"`
		Approvals []run.Approval `json:"approvals"`
	}{threadSummary: summary, ActiveRun: active, Approvals: orEmpty(approvals)})
}

func (a *api) patchThread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	title := ""
	if json.NewDecoder(r.Body).Decode(&body) == nil {
		title = strings.TrimSpace(body.Title)
	}
	if title == "" {
		fail(w, http.StatusBadRequest, "title is required")
		return
	}
	if err := a.st.SetTitle(r.Context(), r.PathValue("id"), title); err != nil {
		a.fail(w, r, err)
		return
	}
	a.getThread(w, r)
}

func (a *api) sendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Text == "" {
		fail(w, http.StatusBadRequest, "text is required")
		return
	}
	m, ru, err := a.runs.Send(r.Context(), r.PathValue("id"), body.Text)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusAccepted, struct {
		Message store.Message `json:"message"`
		Run     store.Run     `json:"run"`
	}{Message: m, Run: ru})
}

func (a *api) listMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := a.st.MessagePage(r.Context(), r.PathValue("id"), r.URL.Query().Get("before"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, orEmpty(msgs))
}

func (a *api) stopRun(w http.ResponseWriter, r *http.Request) {
	if err := a.runs.Stop(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) getRun(w http.ResponseWriter, r *http.Request) {
	ru, err := a.st.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, ru)
}

func (a *api) answer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Approve *bool `json:"approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Approve == nil {
		fail(w, http.StatusBadRequest, "approve is required")
		return
	}
	if err := a.runs.Answer(r.Context(), r.PathValue("id"), *body.Approve); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// events streams the thread's run events. Subscribing comes before the headers are flushed, so a
// client that sees the response misses nothing sent after.
func (a *api) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.st.Thread(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	events, cancel := a.runs.Subscribe(id)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if rc.Flush() != nil {
		return
	}
	ping := time.NewTicker(_pingEvery)
	defer ping.Stop()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, err = fmt.Fprint(w, ": ping\n\n")
		case ev, ok := <-events:
			if !ok {
				return
			}
			var data []byte
			if data, err = json.Marshal(ev.Data); err != nil {
				a.log.Error("encode event", "type", ev.Type, "err", err)
				continue
			}
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
		}
		if err != nil || rc.Flush() != nil {
			return
		}
	}
}

// fail answers err with the status it maps to, and logs it when it is a 500.
func (a *api) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, run.ErrNotFound),
		errors.Is(err, memory.ErrNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, memory.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, run.ErrBusy), errors.Is(err, run.ErrClosed),
		errors.Is(err, store.ErrExists), errors.Is(err, store.ErrUndone),
		errors.Is(err, store.ErrInUse):
		fail(w, http.StatusConflict, err.Error())
	default:
		a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		fail(w, http.StatusInternalServerError, "internal error")
	}
}

func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, struct {
		Error string `json:"error"`
	}{Error: msg})
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// uiHandler serves the built UI; a path that is not a file gets index.html, for client routes.
func uiHandler(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			fail(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, MCPPath) {
			fail(w, http.StatusNotFound, "not found")
			return
		}
		if _, err := fs.Stat(ui, "index.html"); err != nil {
			http.Error(w, "UI not built: run make ui", http.StatusServiceUnavailable)
			return
		}
		if fi, err := fs.Stat(ui, strings.TrimPrefix(r.URL.Path, "/")); err != nil || fi.IsDir() {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
