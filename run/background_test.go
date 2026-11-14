package run

import (
	"testing"

	"atoll/store"
)

func TestFailure(t *testing.T) {
	tests := []struct {
		name string
		give store.InboxItem
		want bool
	}{
		{"helper finished", store.InboxItem{Kind: _helperNotice, Text: "Tester finished thread t1:\nrun failed: no"}, false},
		{"helper failed", store.InboxItem{Kind: _helperNotice, Text: "Tester's run failed: boom\n(thread t1)"}, true},
		{"helper stopped", store.InboxItem{Kind: _helperNotice, Text: "The user stopped Tester\n(thread t1)"}, true},
		{"helper out of limit", store.InboxItem{Kind: _helperNotice, Text: "Tester is out until 2026-11-11T15:42:00Z.\nDo not send it work until then."}, true},
		{"helper deleted", store.InboxItem{Kind: _helperNotice, Text: "Tester was deleted\n(thread t1)"}, false},
		{"restart", store.InboxItem{Kind: "restart", Text: "A restart interrupted 2 helper runs."}, true},
		{"CI passed", store.InboxItem{Kind: "ci", Text: "CI finished on PR #1: 4 passed"}, false},
		{"CI failed", store.InboxItem{Kind: "ci", Text: `CI finished on PR #1: 2 passed, 1 failed ("unit")`}, true},
		{"merged", store.InboxItem{Kind: "merged", Text: "PR #1 merged into main at abc1234"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failure(tt.give); got != tt.want {
				t.Errorf("failure(%+v) = %v, want %v", tt.give, got, tt.want)
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
		"Bhanu": "Reef: background run. Bhanu does not see your final message, so end with one line of status. " +
			"If Bhanu must act (a PR to merge, a decision, a failure you cannot fix), tell them with tell_user.",
		"": "Reef: background run. The user does not see your final message, so end with one line of status. " +
			"If The user must act (a PR to merge, a decision, a failure you cannot fix), tell them with tell_user.",
	} {
		if got := backgroundLine(user); got != want {
			t.Errorf("backgroundLine(%q) = %q, want %q", user, got, want)
		}
	}
}
