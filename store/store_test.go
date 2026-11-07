package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
				_, r, err := st.StartRun(ctx, th.ID, "hi")
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
			for _, id := range want {
				r, err := st.Run(ctx, id)
				if err != nil || r.Status != store.StatusFailed || r.Error != "restart" {
					t.Fatalf("run %s = %+v, %v", id, r, err)
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
		{"bot edit", store.EntityBot, store.ActionEdit, `INSERT INTO bots (id, name, persona, provider) VALUES ('item', 'new', 'new', 'codex')`, `{"id":"item","name":"old","persona":"person","provider":"claude","repo_access":["repo"],"connections":["conn"],"skills":["skill"],"can_manage_platform":true}`, `{"id":"item","name":"old","persona":"person","provider":"claude","mode":"auto","repo_access":["repo"],"connections":["conn"],"skills":["skill"],"mcp_servers":[],"delegates":[],"can_manage_platform":true}`},
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
			undone, err := st.UndoChange(ctx, c.ID)
			if tt.want == "in use" {
				if !errors.Is(err, store.ErrInUse) {
					t.Fatalf("undo = %v, want ErrInUse", err)
				}
				if _, err := st.Repo(ctx, "item"); err != nil {
					t.Fatalf("repo after refused undo: %v", err)
				}
				if _, err := st.UndoChange(ctx, c.ID); !errors.Is(err, store.ErrInUse) {
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
			if _, err := st.UndoChange(ctx, c.ID); !errors.Is(err, store.ErrUndone) {
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
	if _, err := st.UndoChange(ctx, c.ID); err != nil {
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
	_, run, err := st.StartRun(ctx, thread.ID, "hello")
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
		_, run, err := st.StartRun(ctx, thread.ID, "message")
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
	_, r, err := st.StartRun(ctx, th.ID, "hi")
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
	if _, _, err := st.StartRun(ctx, th.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FailUnfinished(ctx, "restart", wakeText); err != nil {
		t.Fatal(err)
	}
	checkWake(t, st, bt.ID, bot, "failed: restart")
}

func TestStore_HelperRunWake_NoBoss(t *testing.T) {
	ctx := context.Background()
	st, bt, bot := wakeFixture(t)
	th, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_, r, err := st.StartRun(ctx, th.ID, "hi")
	if err != nil {
		t.Fatal(err)
	}
	err = st.FinishRun(ctx, r.ID, store.RunResult{Status: store.StatusDone, Reply: "reply"}, wakeText)
	if err != nil {
		t.Fatal(err)
	}
	checkWake(t, st, bt.ID, bot, "")
}
