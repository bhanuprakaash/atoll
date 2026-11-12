package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/store"
)

func TestStore_Open_Private(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateBot(t.Context(), store.Bot{Name: "a", Persona: "p", Provider: "claude"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		dir:                                0o700,
		filepath.Join(dir, "atoll.db"):     0o600,
		filepath.Join(dir, "atoll.db-wal"): 0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode %v, want %v", path, got, want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		source store.TitleSource
		want   string
	}{
		{"generated strips label before cutting", "Status line: One two three four five six seven", store.TitleGenerated, "One two three four five six"},
		{"generated removes repeated stop words", "On the of and to in a for", store.TitleGenerated, ""},
		{"first words keep label", "Status: why does login loop on settings page", store.TitleFirstWords, "Status: why does login loop"},
		{"first words trim when cut", "hey we have to bring the shell tests back.", store.TitleFirstWords, "hey we have to bring"},
		{"stored exact six trims stop word", "Final review of #122 fix on", store.TitleStored, "Final review of #122 fix"},
		{"stored other length stays", "What is this for", store.TitleStored, "What is this for"},
		{"stored keeps punctuation when not cut", "Why is CI red?", store.TitleStored, "Why is CI red?"},
		{"stored cut trims punctuation", "Review of the login loop bug.", store.TitleStored, "Review of the login loop bug"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := store.CleanTitle(tt.text, tt.source); got != tt.want {
				t.Errorf("CleanTitle(%q, %v) = %q, want %q", tt.text, tt.source, got, tt.want)
			}
		})
	}
}

func TestStore_Open_CleansTitles(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ old, want string }{
		{"Conversation title: Login redirect loop", "Login redirect loop"},
		{"Final review of #122 fix on", "Final review of #122 fix"},
		{"What is this for", "What is this for"},
	}
	var ids []string
	for _, tt := range tests {
		thread, err := st.CreateThread(ctx, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetTitle(ctx, thread.ID, tt.old); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, thread.ID)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for i, tt := range tests {
		thread, err := st.Thread(ctx, ids[i])
		if err != nil {
			t.Fatal(err)
		}
		if thread.Title != tt.want {
			t.Errorf("title %q after opening = %q, want %q", tt.old, thread.Title, tt.want)
		}
	}
}

func TestStore_Open_CleansTitlesOnce(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	reopen := func(want string) {
		t.Helper()
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		if st, err = store.Open(dir); err != nil {
			t.Fatal(err)
		}
		got, err := st.Thread(ctx, thread.ID)
		if err != nil || got.Title != want {
			t.Fatalf("title after opening = %q, %v; want %q", got.Title, err, want)
		}
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SetTitle(ctx, thread.ID, "Why is CI red?"); err != nil {
		t.Fatal(err)
	}
	reopen("Why is CI red?")
	reopen("Why is CI red?")
	if err := st.SetTitle(ctx, thread.ID, "Title: Login redirect loop"); err != nil {
		t.Fatal(err)
	}
	reopen("Title: Login redirect loop")
}

func TestRepoAccess_JSON(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		wantAll bool
		want    string
	}{
		{"all", `"all"`, true, `"all"`},
		{"null is all", `null`, true, `"all"`},
		{"list", `["a","b"]`, false, `["a","b"]`},
		{"empty list allows none", `[]`, false, `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got store.RepoAccess
			if err := json.Unmarshal([]byte(tt.give), &got); err != nil {
				t.Fatal(err)
			}
			if (got == nil) != tt.wantAll {
				t.Fatalf("nil = %v, want %v", got == nil, tt.wantAll)
			}
			out, err := json.Marshal(got)
			if err != nil || string(out) != tt.want {
				t.Fatalf("marshal = %s, %v; want %s", out, err, tt.want)
			}
		})
	}
}

func TestStore_FailUnfinished(t *testing.T) {
	tests := []struct {
		name string
		give []string
		want []string
	}{
		{"running and waiting fail", []string{"running", "waiting"}, []string{"running", "waiting"}},
		{"finished stay", []string{"done", "failed", "running"}, []string{"running"}},
		{"none", []string{"done"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			st, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			bot, _ := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
			th, err := st.CreateThread(ctx, bot.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			byStatus := map[string]string{}
			for _, status := range tt.give {
				_, r, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"})
				if err != nil {
					t.Fatal(err)
				}
				switch status {
				case "waiting":
					err = st.SetStatus(ctx, r.ID, store.StatusWaiting)
				case "done", "failed":
					err = st.FinishRun(ctx, r.ID, store.RunResult{Status: status}, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				byStatus[status] = r.ID
			}

			before := time.Now().UTC().Truncate(time.Microsecond)
			got, err := st.FailUnfinished(ctx, "restart", nil)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, status := range tt.want {
				want = append(want, byStatus[status])
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("ids = %v, want %v", got, want)
			}
			if len(want) > 0 {
				threads, err := st.ThreadRuns(ctx, bot.ID)
				if err != nil || len(threads) != 1 || !threads[0].Unseen {
					t.Fatalf("recovered thread = %+v, %v; want New", threads, err)
				}
			}
			for _, id := range want {
				r, err := st.Run(ctx, id)
				if err != nil || r.Status != store.StatusFailed || r.Error != "restart" {
					t.Fatalf("run %s = %+v, %v", id, r, err)
				}
				if r.FinishedAt.Before(before) || r.FinishedAt.After(time.Now()) {
					t.Errorf("finished_at = %v, want recovery time after %v", r.FinishedAt, before)
				}
			}
		})
	}
}

func TestStore_UndoChange(t *testing.T) {
	tests := []struct {
		name, entity, action, setup, before, want string
	}{
		{"bot create", store.EntityBot, store.ActionCreate, `INSERT INTO bots (id, name, persona, provider) VALUES ('item', 'new', 'p', 'claude')`, `null`, `missing`},
		{"bot edit", store.EntityBot, store.ActionEdit, `INSERT INTO repos VALUES ('repo', 'r', '/repo'); INSERT INTO connections VALUES ('conn', 'gitlab', 'https://gitlab.com', 'connected'); INSERT INTO skills (id, name, content) VALUES ('skill', 's', 'text'); INSERT INTO bots (id, name, persona, provider) VALUES ('item', 'new', 'new', 'codex')`, `{"id":"item","name":"old","persona":"person","provider":"claude","repo_access":["repo"],"connections":["conn"],"skills":["skill"],"mcp_servers":["context7"],"can_manage_platform":true}`, `{"id":"item","name":"old","persona":"person","provider":"claude","mode":"auto","repo_access":["repo"],"connections":["conn"],"skills":["skill"],"mcp_servers":["context7"],"delegates":[],"can_manage_platform":true}`},
		{"bot edit drops deleted ids", store.EntityBot, store.ActionEdit, `INSERT INTO bots (id, name, persona, provider) VALUES ('item', 'new', 'new', 'codex'); INSERT INTO bots (id, name, persona, provider, removed) VALUES ('gone', 'gone', '', 'claude', 1)`, `{"name":"old","provider":"claude","repo_access":["gone"],"connections":["gone"],"skills":["gone"],"mcp_servers":["gone"],"delegates":["gone"]}`, `{"id":"item","name":"old","persona":"","provider":"claude","mode":"auto","repo_access":[],"connections":[],"skills":[],"mcp_servers":[],"delegates":[],"can_manage_platform":false}`},
		{"bot edit keeps all repo access", store.EntityBot, store.ActionEdit, `INSERT INTO bots (id, name, persona, provider) VALUES ('item', 'new', 'new', 'codex')`, `{"name":"old","provider":"claude","repo_access":"all"}`, `{"id":"item","name":"old","persona":"","provider":"claude","mode":"auto","repo_access":"all","connections":[],"skills":[],"mcp_servers":[],"delegates":[],"can_manage_platform":false}`},
		{"skill create", store.EntitySkill, store.ActionCreate, `INSERT INTO skills (id, name, content) VALUES ('item', 'new', 'text'); INSERT INTO skill_files VALUES ('item', 'a.sh', 'x', 1)`, `null`, `missing`},
		{"skill edit", store.EntitySkill, store.ActionEdit, `INSERT INTO skills (id, name, content) VALUES ('item', 'new', 'text'); INSERT INTO skill_files VALUES ('item', 'new.md', 'x', 0)`, `{"id":"item","name":"old","content":"original","files":[{"path":"a.sh","content":"x","executable":true}]}`, `{"id":"item","name":"old","content":"original","source":"","files":[{"path":"a.sh","content":"x","executable":true}]}`},
		{"repo create", store.EntityRepo, store.ActionCreate, `INSERT INTO repos VALUES ('item', 'new', '/repo')`, `null`, `missing`},
		{"repo create in use", store.EntityRepo, store.ActionCreate, `INSERT INTO repos VALUES ('item', 'new', '/repo'); INSERT INTO bots (id, name, persona, provider) VALUES ('bot', 'b', 'p', 'claude'); INSERT INTO threads (id, bot_id, created, repo_id) VALUES ('thread', 'bot', 1, 'item')`, `null`, `in use`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			if _, err := st.DB().ExecContext(ctx, tt.setup); err != nil {
				t.Fatal(err)
			}
			c, err := st.RecordChange(ctx, store.Change{Entity: tt.entity, EntityID: "item", Action: tt.action, Before: json.RawMessage(tt.before), After: json.RawMessage(`{}`), SourceThreadID: "thread"})
			if err != nil {
				t.Fatal(err)
			}
			undone, err := st.UndoChange(ctx, c.ID, []string{"context7"})
			if tt.want == "in use" {
				if !errors.Is(err, store.ErrInUse) {
					t.Fatalf("undo = %v, want ErrInUse", err)
				}
				if _, err := st.Repo(ctx, "item"); err != nil {
					t.Fatalf("repo after refused undo: %v", err)
				}
				if _, err := st.UndoChange(ctx, c.ID, []string{"context7"}); !errors.Is(err, store.ErrInUse) {
					t.Fatalf("second undo = %v, want ErrInUse", err)
				}
				return
			}
			if err != nil || !undone.Undone {
				t.Fatalf("undo = %+v, %v", undone, err)
			}
			var value any
			switch tt.entity {
			case store.EntityBot:
				value, err = st.Bot(ctx, "item")
			case store.EntitySkill:
				value, err = st.Skill(ctx, "item")
			case store.EntityRepo:
				value, err = st.Repo(ctx, "item")
			}
			if tt.want == "missing" {
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("read = %v, %v; want missing", value, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(value)
				if err != nil || string(data) != tt.want {
					t.Fatalf("read = %s, %v; want %s", data, err, tt.want)
				}
			}
			if _, err := st.UndoChange(ctx, c.ID, []string{"context7"}); !errors.Is(err, store.ErrUndone) {
				t.Fatalf("second undo = %v", err)
			}
		})
	}
}

func TestStore_UndoCreateBotDropsDelegate(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	made, err := st.CreateBot(ctx, store.Bot{Name: "made", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	boss, err := st.CreateBot(ctx, store.Bot{Name: "boss", Persona: "p", Provider: "claude", Delegates: []string{made.ID, "other"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.RecordChange(ctx, store.Change{Entity: store.EntityBot, EntityID: made.ID, Action: store.ActionCreate, Before: json.RawMessage(`null`), After: json.RawMessage(`{}`), SourceThreadID: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UndoChange(ctx, c.ID, []string{"context7"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Bot(ctx, boss.ID)
	if err != nil || !slices.Equal(got.Delegates, []string{"other"}) {
		t.Fatalf("delegates after undo = %v, %v; want [other]", got.Delegates, err)
	}
}

func TestStore_MessageTimestamps(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: thread.ID, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, run.ID, store.RunResult{Status: store.StatusDone, Reply: "reply"}, nil); err != nil {
		t.Fatal(err)
	}
	finished, err := st.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := st.Messages(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		role string
		want time.Time
	}{{"user", finished.StartedAt}, {"bot", finished.FinishedAt}} {
		t.Run(tt.role, func(t *testing.T) {
			for _, message := range messages {
				if message.Role == tt.role {
					if !message.CreatedAt.Equal(tt.want) || message.CreatedAt.IsZero() {
						t.Fatalf("timestamp = %v, want %v", message.CreatedAt, tt.want)
					}
					return
				}
			}
			t.Fatalf("no %s message", tt.role)
		})
	}
}

func TestStore_MessageRun(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: thread.ID, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReport(ctx, run.ID, store.Report{Done: "Checked it.", Blocked: "Need a key."}); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, run.ID, store.RunResult{Status: store.StatusDone, Reply: "reply"}, nil); err != nil {
		t.Fatal(err)
	}
	all, err := st.Messages(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := st.MessagePage(ctx, thread.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := st.MessagePage(ctx, thread.ID, "", all[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, list := range map[string][]store.Message{"Messages": all, "latest page": latest} {
		if len(list) != 2 {
			t.Fatalf("%s = %d messages, want 2", name, len(list))
		}
	}
	if len(newer) != 1 {
		t.Fatalf("page after the first = %d messages, want 1", len(newer))
	}
	for name, m := range map[string]store.Message{"Messages": all[1], "latest page": latest[1], "after": newer[0]} {
		if m.RunStatus != store.StatusDone || m.Done != "Checked it." || m.Blocked != "Need a key." {
			t.Errorf("%s: run %q done %q blocked %q, want done, %q, %q", name, m.RunStatus, m.Done, m.Blocked, "Checked it.", "Need a key.")
		}
	}
}

func TestStore_MessagePage(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for range 26 {
		_, run, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: thread.ID, Text: "message"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.FinishRun(ctx, run.ID, store.RunResult{Status: store.StatusDone, Reply: "reply"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := st.MessagePage(ctx, thread.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	older, err := st.MessagePage(ctx, thread.ID, latest[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		before, after string
		want          []store.Message
	}{
		{"after returns the newer page", "", older[len(older)-1].ID, latest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.MessagePage(ctx, thread.ID, tt.before, tt.after)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("messages = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID {
					t.Fatalf("message %d = %s, want %s", i, got[i].ID, tt.want[i].ID)
				}
			}
		})
	}
}

func TestStore_ThreadDetails(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMR(ctx, thread.ID, "https://gitlab.example/p/-/merge_requests/43", "merged", "Fix the wizard"); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveWorktree(ctx, thread.ID, "refs/atoll/unfinished/test"); err != nil {
		t.Fatal(err)
	}
	bot.Provider = "codex"
	if _, err := st.UpdateBot(ctx, bot); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSession(ctx, thread.ID, "claude-session", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetClosed(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	got, err := st.Thread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"title", got.MRTitle, "Fix the wizard"},
		{"session", got.Session, "claude-session"},
		{"session provider survives settings change", got.SessionProvider, "claude"},
		{"state", got.MRState, "merged"},
		{"removed", got.WorktreeRemoved, true},
		{"snapshot", got.SnapshotRef, "refs/atoll/unfinished/test"},
		{"closed", got.Closed, true},
		{"closed time", got.ClosedAt != nil && !got.ClosedAt.IsZero(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func wakeFixture(t *testing.T) (*store.Store, store.Thread, store.Bot) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	boss, _ := st.CreateBot(ctx, store.Bot{Name: "boss", Persona: "p", Provider: "claude"})
	bot, _ := st.CreateBot(ctx, store.Bot{Name: "Tester", Persona: "p", Provider: "claude"})
	bt, err := st.CreateThread(ctx, boss.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	return st, bt, bot
}

func wakeText(w store.Wake) string { return w.Run.Status + ": " + w.Run.Reply + w.Run.Error }

func restartText(ws []store.Wake) string { return wakeText(ws[0]) }

func checkWake(t *testing.T, st *store.Store, bossThreadID string, bot store.Bot, want string) {
	t.Helper()
	it, ok, err := st.OldestInbox(context.Background(), bossThreadID)
	if err != nil || ok != (want != "") || it.Text != want || ok && it.FromBotID != bot.ID {
		t.Fatalf("inbox = %+v, %v, %v; want %q from %s", it, ok, err, want, bot.ID)
	}
}

func TestStore_HelperRunWake_Finished(t *testing.T) {
	ctx := context.Background()
	st, bt, bot := wakeFixture(t)
	th, err := st.CreateHelperThread(ctx, bt.ID, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_, r, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	err = st.FinishRun(ctx, r.ID, store.RunResult{Status: store.StatusDone, Reply: "reply"}, wakeText)
	if err != nil {
		t.Fatal(err)
	}
	checkWake(t, st, bt.ID, bot, "done: reply")
}

func TestStore_HelperRunWake_FailedAtRestart(t *testing.T) {
	ctx := context.Background()
	st, bt, bot := wakeFixture(t)
	th, err := st.CreateHelperThread(ctx, bt.ID, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FailUnfinished(ctx, "restart", restartText); err != nil {
		t.Fatal(err)
	}
	checkWake(t, st, bt.ID, bot, "failed: restart")
}

func TestStore_FailUnfinished_OneWakePerBoss(t *testing.T) {
	ctx := context.Background()
	st, bt, bot := wakeFixture(t)
	for range 2 {
		th, err := st.CreateHelperThread(ctx, bt.ID, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(ws []store.Wake) string { return fmt.Sprint(len(ws)) }
	if _, err := st.FailUnfinished(ctx, "restart", count); err != nil {
		t.Fatal(err)
	}
	held, err := st.Held(ctx, bt.ID)
	if err != nil || len(held) != 1 || held[0].Text != "2" || held[0].FromBotID != "" || held[0].Kind != "restart" {
		t.Fatalf("held = %+v, %v; want one restart message of 2 runs from Reef", held, err)
	}
}

func TestStore_RestartWake_KeepsDecisionOpen(t *testing.T) {
	ctx := context.Background()
	st, bt, bot := wakeFixture(t)
	_, ask, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: bt.ID, Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	report := store.Report{Ask: &store.Ask{Question: "Ship it?", Options: []string{"yes", "no"}}}
	if err := st.SetReport(ctx, ask.ID, report); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, ask.ID, store.RunResult{Status: store.StatusDone}, nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		th, err := st.CreateHelperThread(ctx, bt.ID, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.FailUnfinished(ctx, "restart", restartText); err != nil {
		t.Fatal(err)
	}
	held, err := st.Held(ctx, bt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.StartRunFrom(ctx, held...); err != nil {
		t.Fatal(err)
	}
	got, err := st.ThreadWaiting(ctx, bt.ID)
	if err != nil || len(got) != 1 || got[0].Kind != store.WaitDecision || got[0].Question != "Ship it?" {
		t.Fatalf("waiting = %+v, %v; want the open decision", got, err)
	}
}

func TestRealBlocked(t *testing.T) {
	for _, phrase := range []string{"nothing blocking", "nothing blocked", "none", "nothing", "n/a"} {
		for _, suffix := range []string{"", ".", "!", " ."} {
			for _, give := range []string{phrase + suffix, "  " + strings.ToUpper(phrase) + suffix + " "} {
				if store.RealBlocked(give) {
					t.Errorf("RealBlocked(%q) = true, want false", give)
				}
			}
		}
	}
	for _, give := range []string{"", "...", " "} {
		if store.RealBlocked(give) {
			t.Errorf("RealBlocked(%q) = true, want false", give)
		}
	}
	for _, give := range []string{"None of the tests ran: the CI token expired.", "GitLab sign-in expired."} {
		if !store.RealBlocked(give) {
			t.Errorf("RealBlocked(%q) = false, want true", give)
		}
	}
}

func TestStore_ThreadWaiting_StoredNoneLikeBlocked(t *testing.T) {
	ctx := context.Background()
	st, bt, _ := wakeFixture(t)
	_, run, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: bt.ID, Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReport(ctx, run.ID, store.Report{Done: "d", Blocked: "Nothing blocked."}); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, run.ID, store.RunResult{Status: store.StatusDone}, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ThreadWaiting(ctx, bt.ID); err != nil || len(got) != 0 {
		t.Fatalf("waiting = %+v, %v; want nothing", got, err)
	}
}

func TestStore_HelperRunWake_NoBoss(t *testing.T) {
	ctx := context.Background()
	st, bt, bot := wakeFixture(t)
	th, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_, r, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	err = st.FinishRun(ctx, r.ID, store.RunResult{Status: store.StatusDone, Reply: "reply"}, wakeText)
	if err != nil {
		t.Fatal(err)
	}
	checkWake(t, st, bt.ID, bot, "")
}

func TestStore_Attachments(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := st.StartRunFrom(ctx, store.InboxItem{
		ThreadID: thread.ID, Text: "look",
		Images: []store.Attachment{{Mime: "image/png", Data: []byte("png")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := st.Messages(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || len(listed[0].Attachments) != 1 || listed[0].Attachments[0].Data != nil {
		t.Fatalf("listed = %+v, want one message with one attachment and no bytes", listed)
	}
	got, err := st.Attachment(ctx, msgs[0].Attachments[0].ID)
	if err != nil || got.Mime != "image/png" || string(got.Data) != "png" {
		t.Fatalf("attachment = %+v, %v", got, err)
	}
	if _, err := st.DeleteBot(ctx, bot.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Attachment(ctx, got.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after bot delete: err = %v, want ErrNotFound", err)
	}
}

func TestStore_PinThread(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, _ := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
	other, _ := st.CreateBot(ctx, store.Bot{Name: "b", Persona: "p", Provider: "claude"})
	var ids []string
	for range 5 {
		th, err := st.CreateThread(ctx, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, th.ID)
	}
	foreign, _ := st.CreateThread(ctx, other.ID, "")
	helper, _ := st.CreateHelperThread(ctx, ids[0], other.ID, "")
	pinned := func() []string {
		var got []string
		runs, err := st.ThreadRuns(ctx, bot.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			if r.Pinned {
				got = append(got, r.ThreadID)
			}
		}
		slices.Sort(got)
		return got
	}
	sorted := func(s ...string) []string { slices.Sort(s); return s }

	steps := []struct {
		name    string
		do      func() error
		wantErr error
		want    []string
	}{
		{"pin 1", func() error { return st.PinThread(ctx, ids[0], "") }, nil, sorted(ids[0])},
		{"pin 1 again keeps it", func() error { return st.PinThread(ctx, ids[0], "") }, nil, sorted(ids[0])},
		{"pin 2", func() error { return st.PinThread(ctx, ids[1], "") }, nil, sorted(ids[0], ids[1])},
		{"pin 3", func() error { return st.PinThread(ctx, ids[2], "") }, nil, sorted(ids[0], ids[1], ids[2])},
		{"pin 4 is refused", func() error { return st.PinThread(ctx, ids[3], "") }, store.ErrPinLimit,
			sorted(ids[0], ids[1], ids[2])},
		{"pin 4 replacing 2", func() error { return st.PinThread(ctx, ids[3], ids[1]) }, nil,
			sorted(ids[0], ids[2], ids[3])},
		{"replace in another Bot frees nothing", func() error { return st.PinThread(ctx, ids[4], foreign.ID) },
			store.ErrPinLimit, sorted(ids[0], ids[2], ids[3])},
		{"close frees a slot", func() error { return st.SetClosed(ctx, ids[0]) }, nil, sorted(ids[2], ids[3])},
		{"pin 5 after close", func() error { return st.PinThread(ctx, ids[4], "") }, nil,
			sorted(ids[2], ids[3], ids[4])},
		{"unpin", func() error { return st.UnpinThread(ctx, ids[2]) }, nil, sorted(ids[3], ids[4])},
		{"closed thread cannot be pinned", func() error { return st.PinThread(ctx, ids[0], "") },
			store.ErrNotFound, sorted(ids[3], ids[4])},
		{"helper thread cannot be pinned", func() error { return st.PinThread(ctx, helper.ID, "") },
			store.ErrNotFound, sorted(ids[3], ids[4])},
	}
	for _, tt := range steps {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.do(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error %v, want %v", err, tt.wantErr)
			}
			if got := pinned(); !slices.Equal(got, tt.want) {
				t.Errorf("pinned %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStore_ThreadRuns_New(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, _ := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
	th, _ := st.CreateThread(ctx, bot.ID, "")
	state := func() (status string, finished bool) {
		runs, err := st.ThreadRuns(ctx, bot.ID)
		if err != nil || len(runs) != 1 {
			t.Fatalf("ThreadRuns = %v, %v", runs, err)
		}
		r := runs[0]
		return r.Status, r.Unseen
	}
	if status, finished := state(); status != "" || finished {
		t.Errorf("thread without a run: status %q, finished after opened %v; want \"\", false", status, finished)
	}
	_, ru, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if status, finished := state(); status != "running" || finished {
		t.Errorf("running: status %q, finished after opened %v; want running, false", status, finished)
	}
	if err := st.FinishRun(ctx, ru.ID, store.RunResult{Status: store.StatusDone, Reply: "ok"}, nil); err != nil {
		t.Fatal(err)
	}
	if status, finished := state(); status != "done" || !finished {
		t.Errorf("finished: status %q, finished after opened %v; want done, true", status, finished)
	}
	if err := st.MarkOpened(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	if status, finished := state(); status != "done" || finished {
		t.Errorf("opened: status %q, finished after opened %v; want done, false", status, finished)
	}
	_, quiet, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: th.ID, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, quiet.ID, store.RunResult{Status: store.StatusDone}, nil); err != nil {
		t.Fatal(err)
	}
	if status, finished := state(); status != "done" || finished {
		t.Errorf("done with no reply: status %q, finished after opened %v; want done, false", status, finished)
	}
}

func TestStore_Groups(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	names := func() string {
		groups, err := st.Groups(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, g := range groups {
			out = append(out, g.Name)
		}
		return strings.Join(out, ",")
	}
	var ids []string
	for _, n := range []string{"A", "B", "C"} {
		g, err := st.CreateGroup(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, g.ID)
	}
	if got := names(); got != "A,B,C" {
		t.Fatalf("new groups go last: %q, want A,B,C", got)
	}
	moves := []struct {
		name string
		id   string
		up   bool
		want string
	}{
		{"B up", ids[1], true, "B,A,C"},
		{"B up at the top does nothing", ids[1], true, "B,A,C"},
		{"B down", ids[1], false, "A,B,C"},
		{"C down at the end does nothing", ids[2], false, "A,B,C"},
		{"A down", ids[0], false, "B,A,C"},
	}
	for _, tt := range moves {
		t.Run(tt.name, func(t *testing.T) {
			if err := st.MoveGroup(ctx, tt.id, tt.up); err != nil {
				t.Fatal(err)
			}
			if got := names(); got != tt.want {
				t.Errorf("order %q, want %q", got, tt.want)
			}
		})
	}
	if err := st.MoveGroup(ctx, "nope", true); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("move unknown group: %v, want ErrNotFound", err)
	}

	bot, _ := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
	if err := st.SetGroup(ctx, bot.ID, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := st.SetGroup(ctx, bot.ID, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("set unknown group: %v, want ErrNotFound", err)
	}
	if err := st.SetGroupFolded(ctx, ids[0], true); err != nil {
		t.Fatal(err)
	}
	got, err := st.Bot(ctx, bot.ID)
	if err != nil || got.GroupID != ids[0] {
		t.Fatalf("bot group %q, %v; want %q", got.GroupID, err, ids[0])
	}
	if err := st.DeleteGroup(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	got, err = st.Bot(ctx, bot.ID)
	if err != nil || got.GroupID != "" {
		t.Errorf("bot group %q after the group was deleted, %v; want none", got.GroupID, err)
	}
	if got := names(); got != "B,C" {
		t.Errorf("groups %q after delete, want B,C", got)
	}
}

func TestStore_Queued(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	th, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	png := store.Attachment{Mime: "image/png", Data: []byte("png")}
	one, err := st.Enqueue(ctx, store.InboxItem{ThreadID: th.ID, Text: "one", Queued: true, FromUser: true, Images: []store.Attachment{png}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Enqueue(ctx, store.InboxItem{ThreadID: th.ID, Text: "wake", FromBotID: bot.ID}); err != nil {
		t.Fatal(err)
	}
	two, err := st.Enqueue(ctx, store.InboxItem{ThreadID: th.ID, Text: "two", Queued: true, FromUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Attachment(ctx, one.Images[0].ID); err != nil {
		t.Errorf("queued image not served: %v", err)
	}

	queued, err := st.Queued(ctx, th.ID)
	if err != nil || len(queued) != 2 || queued[0].ID != one.ID || queued[1].ID != two.ID || queued[0].Images[0].Data != nil {
		t.Fatalf("Queued = %+v, %v; want one, two without the wake or image bytes", queued, err)
	}
	if _, err := st.EditQueued(ctx, th.ID, two.ID, ""); !errors.Is(err, store.ErrEmpty) {
		t.Errorf("empty edit: %v, want ErrEmpty", err)
	}
	if got, err := st.EditQueued(ctx, th.ID, one.ID, ""); err != nil || got.Text != "" {
		t.Errorf("edit to no text with an image: %+v, %v", got, err)
	}
	if err := st.DeleteQueued(ctx, th.ID, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("delete unknown: %v, want ErrNotFound", err)
	}

	items, err := st.UserInbox(ctx, th.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("UserInbox = %+v, %v; want 2", items, err)
	}
	msgs, _, err := st.StartRunFrom(ctx, items...)
	if err != nil || len(msgs) != 2 || msgs[0].ID != one.ID {
		t.Fatalf("StartRunFrom = %+v, %v; want two messages, the first with id %s", msgs, err, one.ID)
	}
	for _, err := range []error{
		st.DeleteQueued(ctx, th.ID, two.ID),
		st.CheckQueued(ctx, th.ID, one.ID),
		func() error { _, err := st.EditQueued(ctx, th.ID, one.ID, "late"); return err }(),
	} {
		if !errors.Is(err, store.ErrSent) {
			t.Errorf("after delivery: %v, want ErrSent", err)
		}
	}
	if _, err := st.Attachment(ctx, one.Images[0].ID); err != nil {
		t.Errorf("image lost on delivery: %v", err)
	}
	if rest, err := st.Queued(ctx, th.ID); err != nil || len(rest) != 0 {
		t.Errorf("Queued after delivery = %+v, %v; want none", rest, err)
	}
}

func TestStore_StartRunFromRemovedItem(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	th, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var held []store.InboxItem
	for _, text := range []string{"one", "two"} {
		it, err := st.Enqueue(ctx, store.InboxItem{ThreadID: th.ID, Text: text, Queued: true, FromUser: true})
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, it)
	}
	if err := st.DeleteQueued(ctx, th.ID, held[0].ID); err != nil {
		t.Fatal(err)
	}
	msgs, started, err := st.StartRunFrom(ctx, held...)
	if err != nil || len(msgs) != 1 || msgs[0].ID != held[1].ID {
		t.Fatalf("StartRunFrom = %+v, %v; want only the message still held", msgs, err)
	}
	if listed, err := st.Messages(ctx, th.ID); err != nil || len(listed) != 1 || listed[0].Text != "two" {
		t.Errorf("Messages = %+v, %v; want only two", listed, err)
	}

	if err := st.DeleteQueued(ctx, th.ID, held[1].ID); !errors.Is(err, store.ErrSent) {
		t.Fatalf("delete after delivery: %v, want ErrSent", err)
	}
	gone, err := st.Enqueue(ctx, store.InboxItem{ThreadID: th.ID, Text: "gone", Queued: true, FromUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteQueued(ctx, th.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.StartRunFrom(ctx, gone); !errors.Is(err, store.ErrNoneHeld) {
		t.Fatalf("StartRunFrom with nothing held: %v, want ErrNoneHeld", err)
	}
	if listed, err := st.Messages(ctx, th.ID); err != nil || len(listed) != 1 {
		t.Errorf("Messages = %+v, %v; want the one stored message only", listed, err)
	}
	if latest, err := st.LatestRun(ctx, th.ID); err != nil || latest != started.ID {
		t.Errorf("LatestRun = %q, %v; want %q: no run for nothing held", latest, err, started.ID)
	}
}
