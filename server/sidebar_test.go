package server

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"atoll/run"
	"atoll/store"
)

func TestSidebar_Routes(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil)
	bot, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for range 4 {
		th, err := st.CreateThread(ctx, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, th.ID)
	}
	_, ru, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: ids[0], Text: "hello there"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, ru.ID, store.RunResult{Status: store.StatusDone, Reply: "ok"}, nil); err != nil {
		t.Fatal(err)
	}
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:4317"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	pin := func(id string) string { return "/api/threads/" + id + "/pin" }

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"bots list marks the finished thread New", "GET", "/api/bots", "", 200,
			`"threads":[{"id":"` + ids[0] + `","title":"hello there","state":"new","pinned":false,"branch":"","mr_url":"","last_activity":"`},
		{"list has group_id and threads", "GET", "/api/bots", "", 200,
			`"can_manage_platform":false,"group_id":"","threads":[{`},
		{"one Bot has group_id and no threads", "GET", "/api/bots/" + bot.ID, "", 200,
			`"can_manage_platform":false,"group_id":""}`},
		{"open it", "POST", "/api/threads/" + ids[0] + "/opened", "", 204, ""},
		{"opened clears New", "GET", "/api/bots", "", 200,
			`{"id":"` + ids[0] + `","title":"hello there","state":"","pinned":false,`},
		{"pin 1", "POST", pin(ids[0]), "", 204, ""},
		{"pin 2", "POST", pin(ids[1]), `{}`, 204, ""},
		{"pin 3", "POST", pin(ids[2]), "", 204, ""},
		{"pin 4 is refused", "POST", pin(ids[3]), "", 409,
			`{"error":"3 threads of this Bot are pinned"}`},
		{"pin 4 replacing 1", "POST", pin(ids[3]),
			`{"replace":"` + ids[1] + `"}`, 204, ""},
		{"unpin", "DELETE", pin(ids[3]), "", 204, ""},
		{"opened on an unknown thread", "POST", "/api/threads/x/opened", "", 404, ""},
		{"group needs a name", "POST", "/api/groups", `{"name":" "}`, 400, ""},
		{"Chief has no group", "PUT", "/api/bots/chief/group", `{"group_id":null}`, 409, ""},
		{"unknown group", "PUT", "/api/bots/" + bot.ID + "/group", `{"group_id":"x"}`, 404, ""},
		{"move needs up", "POST", "/api/groups/x/move", `{}`, 400, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("status %d body %s, want %d containing %s", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
			}
		})
	}
}

func TestNotices_ParentThread(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := &api{st: st}
	bot, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	boss, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	helper, err := st.CreateHelperThread(ctx, boss.ID, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		threadID string
		want     string
	}{
		{"a boss thread has no parent", boss.ID, ""},
		{"a helper's notice names its boss", helper.ID, boss.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := a.noticeOf(ctx, run.Event{Type: run.EventApproval, ThreadID: tt.threadID})
			if err != nil {
				t.Fatal(err)
			}
			if n.ThreadID != tt.threadID || n.ParentThreadID != tt.want {
				t.Errorf("notice thread %q parent %q, want %q parent %q",
					n.ThreadID, n.ParentThreadID, tt.threadID, tt.want)
			}
		})
	}
}
