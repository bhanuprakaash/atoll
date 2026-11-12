package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"atoll/provider"
	"atoll/run"
	"atoll/store"
)

func TestUndoChange_BotSave(t *testing.T) {
	tests := []struct {
		name, entity, action, setup, before string
	}{
		{
			name: "register repo drops access", entity: store.EntityRepo, action: store.ActionCreate,
			setup: "INSERT INTO repos VALUES ('gone', 'gone', '/repo'); " +
				"UPDATE bots SET repo_access = '[\"gone\"]' WHERE id = 'bot'",
		},
		{
			name: "edit bot drops deleted delegate", entity: store.EntityBot, action: store.ActionEdit,
			setup:  "INSERT INTO bots (id, name, persona, provider) VALUES ('gone', 'Helper', '', 'claude')",
			before: `{"name":"Ada","provider":"claude","repo_access":[],"delegates":["gone"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, st := newHandlerWith(t, nil)
			ctx := t.Context()
			if _, err := st.DB().ExecContext(ctx,
				"INSERT INTO bots (id, name, persona, provider) VALUES ('bot', 'Ada', '', 'claude'); "+tt.setup); err != nil {
				t.Fatal(err)
			}
			id := "gone"
			if tt.entity == store.EntityBot {
				id = "bot"
			}
			c, err := st.RecordChange(ctx, store.Change{Entity: tt.entity, EntityID: id,
				Action: tt.action, Before: json.RawMessage(tt.before), After: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			if tt.entity == store.EntityBot {
				if _, err := st.DeleteBot(ctx, "gone", nil); err != nil {
					t.Fatal(err)
				}
			}
			for _, want := range []int{204, 409} {
				rec := serve(h, http.MethodPost, "/api/platform/changes/"+c.ID+"/undo", "")
				if rec.Code != want {
					t.Fatalf("undo status = %d, body %s; want %d", rec.Code, rec.Body, want)
				}
			}
			rec := serve(h, http.MethodPut, "/api/bots/bot", `{"name":"Ada 2"}`)
			if rec.Code != 200 {
				t.Fatalf("save status = %d, body %s; want 200", rec.Code, rec.Body)
			}
			bot, err := st.Bot(ctx, "bot")
			if err != nil || !slices.Equal(bot.RepoAccess, []string{}) || len(bot.Delegates) != 0 {
				t.Fatalf("bot = %+v, %v; want no repos or delegates", bot, err)
			}
		})
	}
}

func TestUndoChange_CreateBotAlreadyDeleted(t *testing.T) {
	h, st := newHandlerWith(t, nil)
	c, err := st.RecordChange(t.Context(), store.Change{Entity: store.EntityBot, EntityID: "gone",
		Action: store.ActionCreate, After: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{204, 409} {
		rec := serve(h, http.MethodPost, "/api/platform/changes/"+c.ID+"/undo", "")
		if rec.Code != want {
			t.Fatalf("undo status = %d, body %s; want %d", rec.Code, rec.Body, want)
		}
	}
}

func TestUndoChange_CreateBotStopsRun(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.RecordChange(ctx, store.Change{Entity: store.EntityBot, EntityID: bot.ID,
		Action: store.ActionCreate, After: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	sess := &urlSession{events: make(chan provider.Event), url: make(chan string, 1)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runs := run.New(run.Config{Store: st, DataDir: t.TempDir(), Log: log,
		Launch: func(context.Context, string) (provider.Session, provider.Config, error) {
			return sess, provider.Config{}, nil
		}})
	t.Cleanup(func() { _ = runs.Close() })
	h := New(st, nil, runs, log, nil)
	if _, _, err := runs.Send(ctx, thread.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	<-sess.url
	rec := serve(h, http.MethodPost, "/api/platform/changes/"+c.ID+"/undo", "")
	if rec.Code != 204 {
		t.Fatalf("undo status = %d, body %s; want 204", rec.Code, rec.Body)
	}
	if id, _, ok := runs.Active(thread.ID); ok {
		t.Fatalf("run still active: %s", id)
	}
	select {
	case _, ok := <-sess.events:
		if ok {
			t.Fatal("provider events still open")
		}
	default:
		t.Fatal("provider was not interrupted")
	}
	if _, err := st.Bot(ctx, bot.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Bot after undo = %v; want not found", err)
	}
	if _, err := st.Thread(ctx, thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("thread after undo = %v; want not found", err)
	}
	if rec := serve(h, http.MethodPost, "/api/platform/changes/"+c.ID+"/undo", ""); rec.Code != 409 {
		t.Fatalf("second undo status = %d; want 409", rec.Code)
	}
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _ := newHandlerWith(t, nil)
	return h
}

func newHandlerWith(t *testing.T, providers Providers) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, nil, run.New(run.Config{Store: st, Log: log}), log, providers), st
}

func serve(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:4317"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAnswer_RequiresApprove(t *testing.T) {
	tests := []struct {
		name       string
		give       string
		wantStatus int
	}{
		{"no approve field", `{}`, 400},
		{"not JSON", `nope`, 400},
		{"approve true", `{"approve":true}`, 404},
		{"approve false", `{"approve":false}`, 404},
	}
	h := newHandler(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/approvals/x",
				strings.NewReader(tt.give))
			req.Host = "127.0.0.1:4317"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestLocalOnly(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		host       string
		origin     string
		wantStatus int
		token      string
		cookie     string
	}{
		{"127.0.0.1 with a port", "/api/bots", "127.0.0.1:4317", "", 200, "", ""},
		{"localhost", "/api/bots", "localhost:4317", "", 200, "", ""},
		{"IPv6 loopback", "/api/bots", "[::1]:4317", "", 200, "", ""},
		{"same-origin page", "/api/bots", "127.0.0.1:4317", "http://127.0.0.1:4317", 200, "", ""},
		{"rebinding host", "/api/bots", "evil.example:4317", "", 403, "", ""},
		{"other origin", "/api/bots", "127.0.0.1:4317", "https://evil.example", 403, "", ""},
		{"other port as origin", "/api/bots", "127.0.0.1:4317", "http://127.0.0.1:80", 403, "", ""},
		{"null origin", "/api/bots", "127.0.0.1:4317", "null", 403, "", ""},
		{"mcp rebinding host", MCPPath + "tok", "evil.example", "", 403, "", ""},
		{"missing cookie", "/api/bots", "127.0.0.1:4317", "", 401, "secret", ""},
		{"wrong cookie", "/api/bots", "127.0.0.1:4317", "", 401, "secret", "wrong"},
		{"valid cookie", "/api/bots", "127.0.0.1:4317", "", 200, "secret", "secret"},
		{"query cannot authorize API", "/api/bots?token=secret", "127.0.0.1:4317", "", 401, "secret", ""},
		{"cookie cannot bypass origin", "/api/bots", "127.0.0.1:4317", "https://evil.example", 403, "secret", "secret"},
		{"mcp missing cookie", MCPPath + "tok", "127.0.0.1:4317", "", 401, "secret", ""},
		{"mcp valid cookie", MCPPath + "tok", "127.0.0.1:4317", "", 405, "secret", "secret"},
		{"mcp bare endpoint", MCPPath, "127.0.0.1:4317", "", 401, "secret", ""},
		{"token exchange rebinding host", "/?token=secret", "evil.example:4317", "", 403, "secret", ""},
		{"token exchange other origin", "/?token=secret", "127.0.0.1:4317", "https://evil.example", 403, "secret", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ATOLL_TOKEN", tt.token)
			h := newHandler(t)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "atoll_token", Value: tt.cookie})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestLocalOnly_TokenExchange(t *testing.T) {
	t.Setenv("ATOLL_TOKEN", "secret")
	h := newHandler(t)
	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantCookie string
		wantURL    string
	}{
		{"valid token", "secret", 302, "atoll_token=secret; Path=/; HttpOnly; SameSite=Strict", "/"},
		{"wrong token", "wrong", 401, "", ""},
		{"empty token", "", 401, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(h, http.MethodGet, "/?token="+tt.query, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Set-Cookie"); got != tt.wantCookie {
				t.Errorf("Set-Cookie %q, want %q", got, tt.wantCookie)
			}
			if got := rec.Header().Get("Location"); got != tt.wantURL {
				t.Errorf("Location %q, want %q", got, tt.wantURL)
			}
		})
	}
}

func TestGetSettings_DataDir(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log, DataDir: "/data", WorktreeRoot: "/wt"}), log, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/settings", nil)
	req.Host = "127.0.0.1:4317"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	want := `{"worktree_root":"/wt","user_name":"","data_dir":"/data"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}

func TestCreateRepo_UnsafeName(t *testing.T) {
	h := newHandler(t)
	for _, name := range []string{"../x", ".", "..", "a/b"} {
		t.Run(name, func(t *testing.T) {
			rec := serve(h, http.MethodPost, "/api/repos", `{"name":"`+name+`","path":"/tmp"}`)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "name must be one folder name") {
				t.Errorf("status %d, body %s; want 400 about the name", rec.Code, rec.Body)
			}
		})
	}
}

func TestBots_RESTModelAndProvider(t *testing.T) {
	h, st := newHandlerWith(t, fakeProviders{
		{Name: "claude", Enabled: true, SignedIn: true, Models: []Model{{ID: "opus"}, {ID: "sonnet"}}},
		{Name: "codex", SignedIn: true, Models: []Model{{ID: "gpt"}}},
	})
	bot, err := st.CreateBot(t.Context(), store.Bot{Name: "Ada", Provider: "claude", Model: "retired"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		method     string
		path       string
		give       string
		wantStatus int
		wantBody   string
	}{
		{"create with a model the provider does not list", http.MethodPost, "/api/bots", `{"name":"Bo","provider":"claude","model":"gpt"}`, 201, `"model":"gpt"`},
		{"edit to a model the provider does not list", http.MethodPut, "/api/bots/" + bot.ID, `{"model":"gpt"}`, 400, `unknown model \"gpt\"; models: opus, sonnet`},
		{"edit onto a provider that is off", http.MethodPut, "/api/bots/" + bot.ID, `{"provider":"codex"}`, 400, `codex is turned off in Settings`},
		{"create on a provider that is off", http.MethodPost, "/api/bots", `{"name":"Bo","provider":"codex","model":"gpt"}`, 400, `codex is turned off in Settings`},
		{"edit keeping the model it has", http.MethodPut, "/api/bots/" + bot.ID, `{"name":"Ada 2"}`, 200, `"name":"Ada 2"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(h, tt.method, tt.path, tt.give)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("status %d, body %s; want %d containing %s", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
			}
		})
	}
}

// lockProbe writes to the store when asked for providers, and sends the error: the write fails
// when the caller still holds the store's write lock.
type lockProbe struct {
	st   *store.Store
	errs chan error
}

func (*lockProbe) ServeHTTP(http.ResponseWriter, *http.Request) {}

func (p *lockProbe) Cached(ctx context.Context) []Provider {
	p.errs <- p.st.SetSetting(ctx, "probe", "1")
	return nil
}

func TestBotWrites_ReadProvidersOutsideTransaction(t *testing.T) {
	probe := &lockProbe{errs: make(chan error, 1)}
	h, st := newHandlerWith(t, probe)
	probe.st = st
	bot, err := st.CreateBot(t.Context(), store.Bot{Name: "Ada", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(h, http.MethodPut, "/api/bots/"+bot.ID, `{"name":"Ada 2"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT status %d, body %s", rec.Code, rec.Body)
	}
	probed(t, probe, "PUT")
	e := newPlatform(t, probe)
	probe.st = e.st
	e.call(t, "create_bot", `{"name":"Bo","provider":"claude"}`)
	probed(t, probe, "create_bot")
}

func probed(t *testing.T, p *lockProbe, what string) {
	t.Helper()
	select {
	case err := <-p.errs:
		if err != nil {
			t.Errorf("%s holds the write lock while it reads providers: %v", what, err)
		}
	default:
		t.Errorf("%s did not read providers", what)
	}
}

func TestPutBot_ConcurrentPartialSaves(t *testing.T) {
	h, st := newHandlerWith(t, nil)
	bot, err := st.CreateBot(t.Context(), store.Bot{Name: "Ada", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	modes := []string{"ask", "full"}
	for i := range 20 {
		name, mode := "Ada "+strconv.Itoa(i), modes[i%2]
		var wg sync.WaitGroup
		for _, give := range []string{`{"name":"` + name + `"}`, `{"mode":"` + mode + `"}`} {
			wg.Go(func() {
				if rec := serve(h, http.MethodPut, "/api/bots/"+bot.ID, give); rec.Code != http.StatusOK {
					t.Errorf("PUT %s: status %d, body %s", give, rec.Code, rec.Body)
				}
			})
		}
		wg.Wait()
		got, err := st.Bot(t.Context(), bot.ID)
		if err != nil || got.Name != name || got.Mode != mode {
			t.Fatalf("round %d: name %q mode %q (%v), want %q %q", i, got.Name, got.Mode, err, name, mode)
		}
	}
}
