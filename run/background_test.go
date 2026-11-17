package run

import (
	"cmp"
	"context"
	"testing"
	"time"

	"atoll/store"
)

// Every notice kind a wake is written with has a row: a new kind that is a failure needs the guard.
func TestFailure(t *testing.T) {
	helper := func(status, errorKind string) string {
		return store.Wake{Run: store.Run{Status: status, ErrorKind: errorKind}}.Notice()
	}
	tests := []struct {
		name string
		give string
		want bool
	}{
		{"helper finished", helper(store.StatusDone, ""), false},
		{"helper failed", helper(store.StatusFailed, ""), true},
		{"helper stopped", helper(store.StatusStopped, ""), true},
		{"helper out of limit", helper(store.StatusFailed, store.ErrorRateLimited), true},
		{"helper stuck", helper(store.StatusFailed, store.ErrorStuck), true},
		{"helper deleted", store.NoticeHelperDeleted, true},
		{"restart", store.NoticeRestart, true},
		{"CI passed", ciNotice(ciResult{Passed: 4}), false},
		{"CI failed", ciNotice(ciResult{Passed: 2, Failed: 1}), true},
		{"merged", store.NoticeMerged, false},
		{"closed", store.NoticeClosed, false},
		{"person's message", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failure(store.InboxItem{Notice: tt.give}); got != tt.want {
				t.Errorf("failure(notice %q) = %v, want %v", tt.give, got, tt.want)
			}
		})
	}
}

func TestAsNotices(t *testing.T) {
	boss, helper := store.Thread{ID: "boss"}, store.Thread{ID: "helper", ParentThreadID: "boss"}
	person := store.InboxItem{Text: "hi", FromUser: true}
	wake := store.InboxItem{FromBotID: "tester", Text: "Tester finished thread t1:"}
	pr := store.InboxItem{Kind: "merged", Text: "PR #1 merged"}
	tests := []struct {
		name           string
		thread         store.Thread
		give           []store.InboxItem
		wantBackground bool
		wantKinds      []string
	}{
		{"helper wake alone", boss, []store.InboxItem{wake}, true, []string{_helperNotice}},
		{"PR wake alone", boss, []store.InboxItem{pr}, true, []string{"merged"}},
		{"wake and PR", boss, []store.InboxItem{wake, pr}, true, []string{_helperNotice, "merged"}},
		{"a person's message makes the run ordinary", boss, []store.InboxItem{person, wake}, false, []string{"", _helperNotice}},
		{"a message with no sender is the person's", boss, []store.InboxItem{{Text: "hi"}}, false, []string{""}},
		{"a helper thread has no background run", helper, []store.InboxItem{pr}, false, []string{"merged"}},
		{"the brief of a helper is not a notice", helper, []store.InboxItem{{FromBotID: "boss", Text: "Do it."}}, false, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, background := asNotices(tt.thread, tt.give)
			var kinds []string
			for _, it := range got {
				kinds = append(kinds, it.Kind)
			}
			if background != tt.wantBackground || len(kinds) != len(tt.wantKinds) {
				t.Fatalf("asNotices = %v, %v; want %v, %v", kinds, background, tt.wantKinds, tt.wantBackground)
			}
			for i := range kinds {
				if kinds[i] != tt.wantKinds[i] {
					t.Errorf("kinds %v, want %v", kinds, tt.wantKinds)
					break
				}
			}
		})
	}
}

func TestBackgroundLine(t *testing.T) {
	for user, want := range map[string]string{
		"Bhanu": "Reef: background run. Bhanu does not see your final message. " +
			"If Bhanu must act (a PR to merge, a decision, a failure you cannot fix), tell them with tell_user.",
		"": "Reef: background run. The user does not see your final message. " +
			"If The user must act (a PR to merge, a decision, a failure you cannot fix), tell them with tell_user.",
	} {
		if got := backgroundLine(user); got != want {
			t.Errorf("backgroundLine(%q) = %q, want %q", user, got, want)
		}
	}
}

// The guard reads what Reef stored for a held notice, never its words: every wake below says the
// same thing.
func TestFailure_HeldNoticeOfEachWake(t *testing.T) {
	const words = "Reef wrote some other words."
	same := func(store.Wake) string { return words }
	finish := func(res store.RunResult) func(context.Context, *store.Store, string) error {
		return func(ctx context.Context, st *store.Store, runID string) error {
			return st.FinishRun(ctx, runID, res, same)
		}
	}
	tests := []struct {
		name string
		give func(ctx context.Context, st *store.Store, runID string) error
		want bool
	}{
		{"helper finished", finish(store.RunResult{Status: store.StatusDone, Reply: "ok"}), false},
		{"helper failed", finish(store.RunResult{Status: store.StatusFailed, Error: "boom"}), true},
		{"helper stopped", finish(store.RunResult{Status: store.StatusStopped}), true},
		{"helper out of limit", finish(store.RunResult{
			Status: store.StatusFailed, Error: "limit", ErrorKind: store.ErrorRateLimited,
		}), true},
		{"restart cut one helper short", func(ctx context.Context, st *store.Store, _ string) error {
			_, err := st.FailUnfinished(ctx, "restart", func([]store.Wake) string { return words })
			return err
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{})
			boss := bossThread(t, e)
			helper, err := e.st.CreateHelperThread(t.Context(), boss.ID, e.thread.BotID, "")
			if err != nil {
				t.Fatal(err)
			}
			_, ru, err := e.st.StartRunFrom(t.Context(), store.InboxItem{ThreadID: helper.ID, Text: "go"})
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.give(t.Context(), e.st, ru.ID); err != nil {
				t.Fatal(err)
			}
			heldNotice(t, e.st, boss.ID, tt.want)
		})
	}
}

func TestFailure_HeldNoticeOfDeletedHelper(t *testing.T) {
	e := newEnv(t, &fakeSession{})
	e.m.cfg.WakeWindow = time.Hour
	boss := bossThread(t, e)
	helper, err := e.st.CreateHelperThread(t.Context(), boss.ID, e.thread.BotID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.m.Send(t.Context(), helper.ID, "go", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.m.DeleteBot(t.Context(), e.thread.BotID); err != nil {
		t.Fatal(err)
	}
	heldNotice(t, e.st, boss.ID, true)
}

// bossThread is a thread of another Bot, for the helper threads of a test to belong to.
func bossThread(t *testing.T, e *env) store.Thread {
	t.Helper()
	bot, err := e.st.CreateBot(t.Context(), store.Bot{Name: "Boss", Persona: "Terse.", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	boss, err := e.st.CreateThread(t.Context(), bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	return boss
}

func heldNotice(t *testing.T, st *store.Store, bossID string, want bool) {
	t.Helper()
	held, err := st.Held(t.Context(), bossID)
	if err != nil || len(held) != 1 {
		t.Fatalf("held = %+v, %v; want one notice", held, err)
	}
	if got := failure(held[0]); got != want {
		t.Errorf("failure(%+v) = %v, want %v", held[0], got, want)
	}
}

func TestTellUser_ToldOnlyOnceStored(t *testing.T) {
	tests := []struct {
		name       string
		thread     string
		wantErr    bool
		wantToldAt bool
	}{
		{"stored", "", false, true},
		{"not stored", "gone", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{})
			_, ru, err := e.st.StartRunFrom(t.Context(), store.InboxItem{ThreadID: e.thread.ID, Text: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			a := &active{threadID: cmp.Or(tt.thread, e.thread.ID), run: ru, background: true}
			e.m.mu.Lock()
			e.m.tokens["tok"] = a
			e.m.mu.Unlock()
			err = e.m.TellUser(t.Context(), "tok", "Merge #1.")
			e.m.mu.Lock()
			told := a.told
			e.m.mu.Unlock()
			if (err != nil) != tt.wantErr || told != tt.wantToldAt {
				t.Errorf("TellUser = %v, told %v; want error %v, told %v", err, told, tt.wantErr, tt.wantToldAt)
			}
		})
	}
}
