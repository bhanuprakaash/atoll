package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

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
	h := New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil, "")
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

	type row struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Pinned bool   `json:"pinned"`
	}
	t.Run("bots list marks the finished thread New", func(t *testing.T) {
		rec := do("GET", "/api/bots", "")
		var bots []struct {
			ID      string `json:"id"`
			Threads []row  `json:"threads"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &bots); rec.Code != 200 || err != nil {
			t.Fatalf("status %d body %s: %v", rec.Code, rec.Body, err)
		}
		want := []row{{ids[0], "hello there", "new", false}, {ids[3], "", "", false}, {ids[2], "", "", false}, {ids[1], "", "", false}}
		var got []row
		for _, b := range bots {
			if b.ID == bot.ID {
				got = b.Threads
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("threads %+v, want %+v", got, want)
		}
	})

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"list has group_id and threads", "GET", "/api/bots", "", 200,
			`"can_manage_platform":false,"group_id":"","threads":[{`},
		{"one Bot has group_id and no threads", "GET", "/api/bots/" + bot.ID, "", 200,
			`"can_manage_platform":false,"group_id":"","pinned":false}`},
		{"open it", "POST", "/api/threads/" + ids[0] + "/opened", "", 204, ""},
		{"opened clears New", "GET", "/api/bots", "", 200,
			`{"id":"` + ids[0] + `","title":"hello there","state":"","pinned":false,`},
		{"pin 1", "POST", pin(ids[0]), "", 204, ""},
		{"pin 2", "POST", pin(ids[1]), `{}`, 204, ""},
		{"pin 3", "POST", pin(ids[2]), "", 204, ""},
		{"pin 4 is refused", "POST", pin(ids[3]), "", 409,
			`"error":"3 threads of this Bot are pinned","code":"conflict"}`},
		{"pin 4 replacing 1", "POST", pin(ids[3]),
			`{"replace":"` + ids[1] + `"}`, 204, ""},
		{"unpin", "DELETE", pin(ids[3]), "", 204, ""},
		{"opened on an unknown thread", "POST", "/api/threads/x/opened", "", 404, ""},
		{"group needs a name", "POST", "/api/groups", `{"name":" "}`, 400, ""},
		{"Chief has no group", "PUT", "/api/bots/chief/group", `{"group_id":null}`, 409, ""},
		{"unknown group", "PUT", "/api/bots/" + bot.ID + "/group", `{"group_id":"x"}`, 404, ""},
		{"move needs JSON", "POST", "/api/groups/x/move", `nope`, 400, ""},
		{"move an unknown group", "POST", "/api/groups/x/move", `{}`, 404, ""},
		{"Chief is not placed", "PUT", "/api/bots/chief/place", `{"pinned":true}`, 409, ""},
		{"place before an unknown Bot", "PUT", "/api/bots/" + bot.ID + "/place", `{"before":"x"}`, 404, ""},
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

func TestSidebar_ResetsAt(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil, "")
	bot, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	resets := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	tests := []struct {
		name string
		give store.RunResult
		want string
	}{
		{"limited", store.RunResult{Status: store.StatusFailed, ErrorKind: store.ErrorRateLimited, ResetsAt: resets},
			resets.Format(time.RFC3339)},
		{"failed", store.RunResult{Status: store.StatusFailed, Error: "boom"}, ""},
		{"done", store.RunResult{Status: store.StatusDone, Reply: "ok"}, ""},
	}
	for _, tt := range tests {
		th, err := st.CreateThread(ctx, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		_, ru, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: tt.name})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.FinishRun(ctx, ru.ID, tt.give, nil); err != nil {
			t.Fatal(err)
		}
		if err := st.MarkOpened(ctx, th.ID); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequestWithContext(ctx, "GET", "/api/bots", nil)
	req.Host = "127.0.0.1:4317"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var bots []struct {
		ID      string           `json:"id"`
		Threads []map[string]any `json:"threads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bots); rec.Code != 200 || err != nil {
		t.Fatalf("status %d body %s: %v", rec.Code, rec.Body, err)
	}
	got := map[string]string{}
	for _, b := range bots {
		if b.ID != bot.ID {
			continue
		}
		for _, th := range b.Threads {
			title, _ := th["title"].(string)
			at, _ := th["resets_at"].(string)
			if _, has := th["resets_at"]; has != (at != "") {
				t.Errorf("thread %q: resets_at present with value %q", title, at)
			}
			got[title] = at
		}
	}
	for _, tt := range tests {
		if at, ok := got[tt.name]; !ok || at != tt.want {
			t.Errorf("thread %q: resets_at %q (listed %v), want %q", tt.name, at, ok, tt.want)
		}
	}
}
