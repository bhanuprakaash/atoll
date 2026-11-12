// Package server is Atoll's HTTP API: REST and a per-thread SSE stream under /api/, Atoll's MCP
// tools under /mcp/, and the UI at /.
package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"atoll/mcpserver"
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
	_signInExpiry  = 10 * time.Minute
	// RFC 8628 §3.2: the interval when the device authorization reply has none.
	_defaultDeviceInterval = 5 * time.Second
	_chiefID               = "chief"
)

//go:embed all:ui
var _ui embed.FS

type api struct {
	st   *store.Store
	mem  *memory.Store
	runs *run.Manager
	log  *slog.Logger
	http *http.Client

	providers Providers // serves /api/providers; nil when there is none

	mu      sync.Mutex
	devices map[string]*device
	signIns map[string]mcpserver.Pending
}

// device is a pending sign-in. GitHub and GitLab answer slow_down to a poll sooner than interval
// after the previous one, and to every later poll at the old pace, so the server keeps the pace
// for all its callers.
type device struct {
	code     string
	interval time.Duration
	next     time.Time
}

// New returns the handler for the API, the MCP tools and the UI. providers, when not nil, serves
// /api/providers.
func New(
	st *store.Store, mem *memory.Store, runs *run.Manager, log *slog.Logger, providers Providers,
) http.Handler {
	a := &api{
		st: st, mem: mem, runs: runs, log: log, providers: providers,
		http:    &http.Client{Timeout: _signInTimeout},
		devices: map[string]*device{},
		signIns: map[string]mcpserver.Pending{},
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
	mux.HandleFunc("POST /api/connections/gitlab", a.startGitLab)
	mux.HandleFunc("POST /api/connections/github/start", a.startGitHub)
	mux.HandleFunc("POST /api/connections/github", a.startGitHub)
	mux.HandleFunc("POST /api/connections/{id}/poll", a.pollConnection)
	mux.HandleFunc("GET /api/connections/{id}", a.getConnection)
	mux.HandleFunc("POST /api/bots", a.createBot)
	mux.HandleFunc("GET /api/bots", a.listBots)
	mux.HandleFunc("GET /api/bots/{id}", a.getBot)
	mux.HandleFunc("PUT /api/bots/{id}", a.putBot)
	mux.HandleFunc("DELETE /api/bots/{id}", a.deleteBot)
	mux.HandleFunc("PUT /api/bots/{id}/group", a.setBotGroup)
	mux.HandleFunc("GET /api/groups", a.listGroups)
	mux.HandleFunc("POST /api/groups", a.createGroup)
	mux.HandleFunc("PATCH /api/groups/{id}", a.patchGroup)
	mux.HandleFunc("POST /api/groups/{id}/move", a.moveGroup)
	mux.HandleFunc("DELETE /api/groups/{id}", a.deleteGroup)
	mux.HandleFunc("POST /api/bots/{id}/pin", a.pinBot)
	mux.HandleFunc("DELETE /api/bots/{id}/pin", a.unpinBot)
	mux.HandleFunc("POST /api/skills", a.createSkill)
	mux.HandleFunc("GET /api/skills", a.listSkills)
	mux.HandleFunc("GET /api/skills/{id}", a.getSkill)
	mux.HandleFunc("PUT /api/skills/{id}", a.putSkill)
	mux.HandleFunc("POST /api/skills/{id}/reimport", a.reimportSkill)
	mux.HandleFunc("GET /api/mcp-servers", a.listMCPServers)
	mux.HandleFunc("POST /api/mcp-servers", a.createMCPServer)
	mux.HandleFunc("PUT /api/mcp-servers/{id}", a.putMCPServer)
	mux.HandleFunc("DELETE /api/mcp-servers/{id}", a.deleteMCPServer)
	mux.HandleFunc("POST /api/mcp-servers/{id}/connect", a.connectMCPServer)
	mux.HandleFunc("GET /api/mcp-servers/{id}/setup", a.mcpServerSetup)
	mux.HandleFunc("POST /api/mcp-servers/{id}/setup", a.saveMCPServerSetup)
	mux.HandleFunc("GET /oauth/callback", a.oauthCallback)
	mux.HandleFunc("POST /api/platform/changes/{id}/undo", a.undoChange)
	mux.HandleFunc("GET /api/threads/{id}/changes", a.listChanges)
	mux.HandleFunc("POST /api/bots/{id}/threads", a.createThread)
	mux.HandleFunc("GET /api/bots/{id}/threads", a.listThreads)
	mux.HandleFunc("GET /api/threads/{id}", a.getThread)
	mux.HandleFunc("PATCH /api/threads/{id}", a.patchThread)
	mux.HandleFunc("POST /api/threads/{id}/close", a.closeThread)
	mux.HandleFunc("POST /api/threads/{id}/opened", a.markOpened)
	mux.HandleFunc("POST /api/threads/{id}/pin", a.pinThread)
	mux.HandleFunc("DELETE /api/threads/{id}/pin", a.unpinThread)
	mux.HandleFunc("POST /api/threads/{id}/messages", a.sendMessage)
	mux.HandleFunc("GET /api/threads/{id}/messages", a.listMessages)
	mux.HandleFunc("PATCH /api/threads/{id}/messages/{mid}", a.editQueued)
	mux.HandleFunc("DELETE /api/threads/{id}/messages/{mid}", a.removeQueued)
	mux.HandleFunc("POST /api/threads/{id}/messages/{mid}/send", a.sendQueued)
	mux.HandleFunc("GET /api/threads/{id}/events", a.events)
	mux.HandleFunc("GET /api/events", a.notices)
	mux.HandleFunc("POST /api/island/seen", a.markIslandSeen)
	mux.HandleFunc("GET /api/threads/{id}/file", a.threadFile)
	mux.HandleFunc("GET /api/attachments/{id}", a.getAttachment)
	mux.HandleFunc("GET /api/runs/{id}", a.getRun)
	mux.HandleFunc("POST /api/runs/{id}/stop", a.stopRun)
	mux.HandleFunc("POST /api/approvals/{id}", a.answer)
	mux.HandleFunc(MCPPath+"{token}", a.mcp)
	// fs.Sub fails only for an invalid dir name.
	sub, _ := fs.Sub(_ui, "ui")
	mux.Handle("/", uiHandler(sub))
	return a.localOnly(mux)
}

// localOnly refuses an API or MCP request that is not for a local host, or that a page on
// another origin sent: otherwise any web page could drive Bots, which run code.
func (a *api) localOnly(next http.Handler) http.Handler {
	token := os.Getenv("ATOLL_TOKEN")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, MCPPath)
		exchange := token != "" && r.Method == http.MethodGet && r.URL.Path == "/" &&
			r.URL.Query().Has("token")
		if (guarded || exchange) && (!isLocalHost(r.Host) || !sameOrigin(r)) {
			fail(w, http.StatusForbidden, "forbidden")
			return
		}
		if exchange {
			if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(token)) != 1 {
				fail(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: "atoll_token", Value: token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if token != "" && guarded {
			cookie, err := r.Cookie("atoll_token")
			authenticated := err == nil &&
				subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) == 1
			if !authenticated && strings.HasPrefix(r.URL.Path, MCPPath) {
				_, _, _, err := a.runs.Source(strings.TrimPrefix(r.URL.Path, MCPPath))
				authenticated = err == nil
			}
			if !authenticated {
				fail(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next.ServeHTTP(w, r)
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
	if msg := botProblem(b); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	msg, err := a.restPicksProblem(r.Context(), b, store.Bot{})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	b, err = createWithLook(r.Context(), a.st, b)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, responseBot(b))
}

func (a *api) listBots(w http.ResponseWriter, r *http.Request) {
	responses, threads, helpers, err := a.botList(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for i, resp := range responses {
		responses[i].Threads = a.sidebarThreads(threads[resp.ID], helpers[resp.ID])
	}
	write(w, http.StatusOK, responses)
}

func (a *api) botList(
	ctx context.Context,
) (responses []botResponse, threads, helpers map[string][]store.ThreadRun, err error) {
	bots, err := a.st.Bots(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	threads, helpers, err = a.threadRuns(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	responses = make([]botResponse, 0, len(bots))
	for _, b := range bots {
		responses = append(responses, responseBot(a.botSummary(ctx, b, threads[b.ID], helpers[b.ID])))
	}
	return responses, threads, helpers, nil
}

func (a *api) threadRuns(ctx context.Context) (threads, helpers map[string][]store.ThreadRun, err error) {
	runs, err := a.st.PollThreadRuns(ctx)
	if err != nil {
		return nil, nil, err
	}
	owner := map[string]string{}
	for _, tr := range runs {
		owner[tr.ThreadID] = tr.BotID
	}
	threads, helpers = map[string][]store.ThreadRun{}, map[string][]store.ThreadRun{}
	for _, tr := range runs {
		threads[tr.BotID] = append(threads[tr.BotID], tr)
		if boss := owner[tr.ParentThreadID]; boss != "" {
			helpers[boss] = append(helpers[boss], tr)
		}
	}
	return threads, helpers, nil
}

func (a *api) createThread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoID string      `json:"repo_id"`
		Text   string      `json:"text"`
		Images []imageBody `json:"images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	images, msg := decodeImages(body.Images)
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
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
	if body.Text != "" || len(images) > 0 {
		m, ru, err := a.runs.Send(r.Context(), t.ID, body.Text, images)
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
	RunID        string        `json:"run_id"`
	RunStatus    string        `json:"run_status"`
	RunErrorKind string        `json:"run_error_kind"`
	Approval     *run.Approval `json:"approval"`
	Verb         string        `json:"verb"`
	UpdatedAt    time.Time     `json:"updated_at"`

	Step        string          `json:"step"`
	ParentBotID string          `json:"parent_bot_id"`
	Helpers     []threadSummary `json:"helpers"`
}

func (a *api) threadSummary(r *http.Request, t store.Thread) (threadSummary, error) {
	t, err := a.st.Thread(r.Context(), t.ID)
	if err != nil {
		return threadSummary{}, err
	}
	out := threadSummary{Thread: t, UpdatedAt: t.LastActivity}
	if t.ParentThreadID != "" {
		boss, err := a.st.Thread(r.Context(), t.ParentThreadID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return out, err
		}
		out.ParentBotID = boss.BotID
	}
	helpers, err := a.st.HelperThreads(r.Context(), t.ID)
	if err != nil {
		return out, err
	}
	out.Helpers = make([]threadSummary, 0, len(helpers))
	for _, h := range helpers {
		s, err := a.threadSummary(r, h)
		if err != nil {
			return out, err
		}
		out.Helpers = append(out.Helpers, s)
	}
	out.Verb = a.liveVerb(t.ID)
	out.Step = a.liveStep(t.ID)
	out.RunID, err = a.st.LatestRun(r.Context(), t.ID)
	if err != nil {
		return out, err
	}
	if out.RunID != "" {
		ru, err := a.st.Run(r.Context(), out.RunID)
		if err != nil {
			return out, err
		}
		out.RunStatus, out.RunErrorKind = ru.Status, ru.ErrorKind
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

func (a *api) liveStep(threadID string) string {
	tool := a.runs.ActiveTool(threadID)
	target := tool.Target
	var in struct {
		Command string `json:"command"`
	}
	if tool.Name == "Bash" && json.Unmarshal(tool.Input, &in) == nil && in.Command != "" {
		target = run.CommandTarget(in.Command)
	}
	return strings.TrimSpace(tool.Verb + " " + target)
}

func (a *api) listThreads(w http.ResponseWriter, r *http.Request) {
	if _, err := a.st.Bot(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
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
	queued, err := a.st.Queued(r.Context(), t.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	waiting := make([]store.Message, len(queued))
	for i, it := range queued {
		waiting[i] = queuedMessage(it)
	}
	write(w, http.StatusOK, struct {
		threadSummary
		ActiveRun *store.Run      `json:"active_run"`
		Approvals []run.Approval  `json:"approvals"`
		Queued    []store.Message `json:"queued"`
	}{threadSummary: summary, ActiveRun: active, Approvals: orEmpty(approvals), Queued: waiting})
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
		Text   string      `json:"text"`
		Images []imageBody `json:"images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		(body.Text == "" && len(body.Images) == 0) {
		fail(w, http.StatusBadRequest, "text or an image is required")
		return
	}
	images, msg := decodeImages(body.Images)
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	id := r.PathValue("id")
	if !a.userMayWrite(w, r, id) {
		return
	}
	if len(images) == 0 {
		replied, err := a.runs.Reply(r.Context(), id, body.Text)
		if err == nil {
			write(w, http.StatusAccepted, struct {
				Message  store.Message `json:"message"`
				Run      *store.Run    `json:"run"`
				Answered []string      `json:"answered"`
			}{Message: replied.Message, Run: replied.Run, Answered: replied.Answered})
			return
		}
		if !errors.Is(err, run.ErrNoApproval) {
			a.fail(w, r, err)
			return
		}
	}
	m, ru, err := a.runs.Send(r.Context(), id, body.Text, images)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var started *store.Run
	if !m.Queued {
		started = &ru
	}
	write(w, http.StatusAccepted, struct {
		Message store.Message `json:"message"`
		Run     *store.Run    `json:"run"`
	}{Message: m, Run: started})
}

// userMayWrite answers 409 and returns false when the thread is a helper thread whose boss is
// handling it.
func (a *api) userMayWrite(w http.ResponseWriter, r *http.Request, threadID string) bool {
	t, err := a.st.Thread(r.Context(), threadID)
	if err != nil || t.ParentThreadID == "" {
		return true
	}
	boss, err := a.bossName(r.Context(), t.ParentThreadID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.fail(w, r, err)
		return false
	}
	if err == nil {
		fail(w, http.StatusConflict, boss+" is handling this thread")
		return false
	}
	return true
}

func (a *api) editQueued(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "text or an image is required")
		return
	}
	id := r.PathValue("id")
	if !a.userMayWrite(w, r, id) {
		return
	}
	it, err := a.runs.EditQueued(r.Context(), id, r.PathValue("mid"), body.Text)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, queuedMessage(it))
}

func queuedMessage(it store.InboxItem) store.Message {
	return store.Message{ID: it.ID, ThreadID: it.ThreadID, Role: store.RoleUser, Text: it.Text,
		FromBotID: it.FromBotID, CreatedAt: it.CreatedAt, Queued: true, Attachments: orEmpty(it.Images)}
}

func (a *api) removeQueued(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !a.userMayWrite(w, r, id) {
		return
	}
	if err := a.runs.RemoveQueued(r.Context(), id, r.PathValue("mid")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) sendQueued(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !a.userMayWrite(w, r, id) {
		return
	}
	ru, err := a.runs.SendNow(r.Context(), id, r.PathValue("mid"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusAccepted, struct {
		Run store.Run `json:"run"`
	}{Run: ru})
}

func (a *api) bossName(ctx context.Context, threadID string) (string, error) {
	t, err := a.st.Thread(ctx, threadID)
	if err != nil {
		return "", err
	}
	b, err := a.st.Bot(ctx, t.BotID)
	return b.Name, err
}

func (a *api) listMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := a.st.MessagePage(r.Context(), r.PathValue("id"), r.URL.Query().Get("before"), r.URL.Query().Get("after"))
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
	if ru.ErrorKind == store.ErrorProviderOff {
		name := strings.ToUpper(ru.Provider[:1]) + ru.Provider[1:]
		ru.ErrorDetail, ru.Error = ru.Error, name+" isn't installed or isn't signed in."
	}
	write(w, http.StatusOK, ru)
}

func (a *api) answer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Approve  *bool `json:"approve"`
		InThread bool  `json:"in_thread"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Approve == nil {
		fail(w, http.StatusBadRequest, "approve is required")
		return
	}
	answer := a.runs.Answer
	if body.InThread {
		answer = a.runs.AnswerInThread
	}
	if err := answer(r.Context(), r.PathValue("id"), *body.Approve); err != nil {
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
	a.stream(w, r, events, func(ev run.Event) (any, bool) { return ev.Data, true })
}

// sseEvent is one server-sent event.
type sseEvent struct {
	Type string
	Data any
}

// stream writes events as server-sent events until the client goes or events closes. data gives
// an event's payload, or false to skip it. The lead events are written first.
func (a *api) stream(
	w http.ResponseWriter, r *http.Request, events <-chan run.Event,
	data func(run.Event) (any, bool), lead ...sseEvent,
) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if rc.Flush() != nil {
		return
	}
	for _, ev := range lead {
		line, err := json.Marshal(ev.Data)
		if err == nil {
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, line)
		}
		if err != nil || rc.Flush() != nil {
			return
		}
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
			payload, ok := data(ev)
			if !ok {
				continue
			}
			var line []byte
			if line, err = json.Marshal(payload); err != nil {
				a.log.Error("encode event", "type", ev.Type, "err", err)
				continue
			}
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, line)
		}
		if err != nil || rc.Flush() != nil {
			return
		}
	}
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
