package run

import (
	"slices"
	"testing"
	"time"

	"atoll/store"
)

func TestPick_Holds(t *testing.T) {
	person := func(text string) store.InboxItem { return store.InboxItem{Text: text, FromUser: true} }
	notice := func(text, kind string) store.InboxItem {
		return store.InboxItem{Text: text, FromBotID: "bot", Notice: kind}
	}
	release := func(text string) store.InboxItem { return notice(text, store.NoticeRelease) }
	tests := []struct {
		name   string
		helper bool
		hold   bool
		give   []store.InboxItem
		// stopAfter holds the thread after the items are enqueued: held release markers go.
		stopAfter bool
		want      []string
	}{
		{"lead holds helper notices", false, true, []store.InboxItem{
			notice("done", store.NoticeHelperDone), notice("failed", store.NoticeHelperFailed),
			notice("stopped", store.NoticeHelperStopped), notice("limited", store.NoticeHelperLimited),
			notice("deleted", store.NoticeHelperDeleted), notice("restart", store.NoticeRestart),
		}, false, nil},
		{"lead takes CI and PR notices", false, true, []store.InboxItem{
			notice("done", store.NoticeHelperDone), notice("ci", store.NoticeCIPassed),
			notice("merged", store.NoticeMerged), notice("closed", store.NoticeClosed),
		}, false, []string{"ci", "merged", "closed"}},
		{"person releases a lead's held notices", false, true, []store.InboxItem{
			notice("done", store.NoticeHelperDone), person("hi"), notice("failed", store.NoticeHelperFailed),
		}, false, []string{"hi", "done", "failed"}},
		{"lead that holds nothing takes notices", false, false, []store.InboxItem{
			notice("done", store.NoticeHelperDone), notice("ci", store.NoticeCIPassed),
		}, false, []string{"done", "ci"}},
		{"stopped helper holds a message with no marker", true, true, []store.InboxItem{
			notice("one", ""), notice("two", store.NoticeMerged),
		}, false, nil},
		{"stopped helper takes the whole batch on a release", true, true, []store.InboxItem{
			notice("one", ""), release("go"), notice("three", store.NoticeMerged),
		}, false, []string{"one", "go", "three"}},
		{"stopped helper strips a marker already held", true, false, []store.InboxItem{
			release("go"), notice("two", ""),
		}, true, nil},
		{"stopped helper takes the person's messages only", true, true, []store.InboxItem{
			person("hi"), notice("one", ""), person("again"),
		}, false, []string{"hi", "again"}},
		{"running helper takes one message", true, false, []store.InboxItem{
			notice("one", ""), notice("two", ""),
		}, false, []string{"one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{})
			thread := bossThread(t, e)
			if tt.helper {
				var err error
				thread, err = e.st.CreateHelperThread(t.Context(), thread.ID, e.thread.BotID, "")
				if err != nil {
					t.Fatal(err)
				}
			}
			hold := func() {
				if err := e.st.HoldReports(t.Context(), thread.ID); err != nil {
					t.Fatal(err)
				}
			}
			if tt.hold {
				hold()
			}
			for _, it := range tt.give {
				it.ThreadID = thread.ID
				if _, err := e.st.Enqueue(t.Context(), it); err != nil {
					t.Fatal(err)
				}
			}
			if tt.stopAfter {
				hold()
			}
			items, err := e.m.pick(t.Context(), thread.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, it := range items {
				got = append(got, it.Text)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("pick = %v, want %v", got, tt.want)
			}
		})
	}
}

// A boss's message marks itself a release: it takes what a Stop left held for the helper.
func TestPost_ReleasesStoppedHelper(t *testing.T) {
	e := newEnv(t, &fakeSession{})
	boss := bossThread(t, e)
	helper, err := e.st.CreateHelperThread(t.Context(), boss.ID, e.thread.BotID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.HoldReports(t.Context(), helper.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Enqueue(t.Context(), store.InboxItem{ThreadID: helper.ID, Text: "earlier", FromBotID: "bot"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.post(t.Context(), helper.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	if held, err := e.st.Held(t.Context(), helper.ID); err != nil || len(held) != 0 {
		t.Errorf("held = %+v, %v; want none after the continue's run started", held, err)
	}
}

func TestContinueNotice_ReasonIsTheRunLines(t *testing.T) {
	for _, c := range []struct{ kind, err, reason string }{
		{"", "claude exited: exit status 1", "something went wrong"},
		{store.ErrorRestarted, "Reef restarted", "Reef restarted"},
		{store.ErrorStuck, "Stopped: no progress for 15 minutes.", "no progress for 15 minutes"},
		{store.ErrorRateLimited, "Stopped: limit.", "something went wrong"},
	} {
		want := "Your last run stopped before it finished (" + c.reason + "). Continue from where you stopped. " +
			"Check what already happened before you repeat a step."
		if got := continueNotice(store.Run{ErrorKind: c.kind, Error: c.err}); got != want {
			t.Errorf("%q: notice %q, want %q", c.kind, got, want)
		}
	}
}

func TestStoppedBlock(t *testing.T) {
	tests := []struct {
		name string
		w    store.Wake
		g    GitState
		want string
	}{
		{
			"branch, ahead and dirty",
			store.Wake{Helper: "Tester", ThreadID: "t1", Branch: "feat/x"}, GitState{Ahead: 3, Dirty: true},
			"Tester stopped: no progress for 15m.\nBranch feat/x, 3 commits ahead, uncommitted changes. (thread t1)",
		},
		{
			"one commit",
			store.Wake{Helper: "Tester", ThreadID: "t1", Branch: "feat/x"}, GitState{Ahead: 1},
			"Tester stopped: no progress for 15m.\nBranch feat/x, 1 commit ahead. (thread t1)",
		},
		{
			"no commits",
			store.Wake{Helper: "Tester", ThreadID: "t1", Branch: "feat/x"}, GitState{},
			"Tester stopped: no progress for 15m.\nBranch feat/x, 0 commits ahead. (thread t1)",
		},
		{
			"dirty, nothing ahead",
			store.Wake{Helper: "Tester", ThreadID: "t1", Branch: "feat/x"}, GitState{Dirty: true},
			"Tester stopped: no progress for 15m.\nBranch feat/x, 0 commits ahead, uncommitted changes. (thread t1)",
		},
		{
			"no branch",
			store.Wake{Helper: "Tester", ThreadID: "t1"}, GitState{Ahead: 2, Dirty: true},
			"Tester stopped: no progress for 15m.\nNo branch yet. (thread t1)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stoppedBlock(tt.w, tt.g, "no progress for 15m"); got != tt.want {
				t.Errorf("stoppedBlock = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShortDuration(t *testing.T) {
	tests := []struct {
		give time.Duration
		want string
	}{
		{15 * time.Minute, "15m"},
		{2 * time.Hour, "2h"},
		{time.Hour + 10*time.Minute, "1h10m"},
		{time.Hour + 5*time.Second, "1h0m5s"},
		{90 * time.Second, "1m30s"},
		{30 * time.Second, "30s"},
		{100 * time.Millisecond, "100ms"},
	}
	for _, tt := range tests {
		if got := shortDuration(tt.give); got != tt.want {
			t.Errorf("shortDuration(%v) = %q, want %q", tt.give, got, tt.want)
		}
	}
}

func TestPlainDuration(t *testing.T) {
	tests := []struct {
		give time.Duration
		want string
	}{
		{time.Second, "1 second"},
		{2 * time.Second, "2 seconds"},
		{90 * time.Second, "90 seconds"},
		{time.Minute, "1 minute"},
		{15 * time.Minute, "15 minutes"},
		{90 * time.Minute, "90 minutes"},
		{time.Hour, "1 hour"},
		{2 * time.Hour, "2 hours"},
	}
	for _, tt := range tests {
		if got := plainDuration(tt.give); got != tt.want {
			t.Errorf("plainDuration(%v) = %q, want %q", tt.give, got, tt.want)
		}
	}
}
