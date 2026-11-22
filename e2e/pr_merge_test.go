package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// prMerge is POST /api/threads/{id}/pr/merge {url, sha} and the status it answered with.
func (f *prWakeFixture) prMerge(t *testing.T, th thread, pull, sha string) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"url": pull, "sha": sha})
	req, err := http.NewRequestWithContext(t.Context(), "POST", f.s.url+"/api/threads/"+th.ID+"/pr/merge", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// A merge sends the head sha the caller loaded and the first method the repo allows (merge commit,
// squash, rebase), with the Bot's connection. The thread's PR check runs at once: with a poll
// interval of an hour, only that check can turn the thread's PR merged, and it wakes the Bot.
func TestPRMerge_MergesTheLoadedHeadWithTheFirstAllowedMethod(t *testing.T) {
	for _, c := range []struct {
		name                  string
		merge, squash, rebase bool
		method                string
	}{
		{"all allowed", true, true, true, "merge"},
		{"squash only", false, true, false, "squash"},
		{"rebase only", false, false, true, "rebase"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newPRWakeFixture(t, "claude", "ATOLL_REPO_POLL_INTERVAL=1h")
			th := f.openPR(t, 1)
			f.gh.setMergeMethods(c.merge, c.squash, c.rebase)

			var reply struct {
				State string `json:"state"`
			}
			status, body := f.prMerge(t, th, f.gh.URL+"/octo/hello/pull/1", headSHA(1))
			_ = json.Unmarshal(body, &reply)
			if status != http.StatusOK || reply.State != "merged" {
				t.Fatalf("POST pr/merge: status %d: %s, want 200 {\"state\":\"merged\"}", status, body)
			}
			got := f.gh.merges(1)
			if len(got) != 1 || got[0].SHA != headSHA(1) || got[0].Method != c.method || !got[0].Merged {
				t.Errorf("merge requests %+v, want one with sha %s and merge_method %s that merged", got, headSHA(1), c.method)
			}
			eventually(t, 10*time.Second, "the thread's PR is merged without waiting for the hourly poll", func() bool {
				return f.mrState(t, th) == "merged"
			})
			w := f.waitWake(t, th, 1)
			f.wantWake(t, th, w, "merged", "PR #1 merged")
		})
	}
}

// A sha that is no longer the head is refused by GitHub (409): the caller gets head_moved with the
// plain sentence, and nothing is merged. A pull request GitHub will not merge answers refused with
// GitHub's own message, no JSON; nothing is merged and the thread's PR stays open.
func TestPRMerge_Refusals(t *testing.T) {
	f := newPRWakeFixture(t, "claude", "ATOLL_REPO_POLL_INTERVAL=1h")
	th := f.openPR(t, 1)
	pull := f.gh.URL + "/octo/hello/pull/1"
	loaded := headSHA(1)

	f.gh.pushHead(1)
	status, body := f.prMerge(t, th, pull, loaded)
	var e prReadError
	_ = json.Unmarshal(body, &e)
	if want := "New commits were pushed since you opened this PR. Nothing was merged."; status != http.StatusConflict || e.Code != "head_moved" || e.Error != want {
		t.Errorf("stale sha: status %d %s, want 409 code head_moved error %q", status, body, want)
	}
	if got := f.gh.merges(1); len(got) != 1 || got[0].SHA != loaded || got[0].Merged {
		t.Errorf("after a stale sha: merge requests %+v, want one for the stale sha, not merged", got)
	}

	f.gh.setHead(1, loaded)
	f.gh.setConflicts(1, true)
	status, body = f.prMerge(t, th, pull, loaded)
	e = prReadError{}
	_ = json.Unmarshal(body, &e)
	if status != http.StatusConflict || e.Code != "refused" || e.Error != "Pull Request is not mergeable" {
		t.Errorf("not mergeable: status %d %s, want 409 code refused error %q", status, body, "Pull Request is not mergeable")
	}
	for _, m := range f.gh.merges(1) {
		if m.Merged {
			t.Errorf("merge requests %+v, want none merged", f.gh.merges(1))
		}
	}
	if got := f.mrState(t, th); got == "merged" {
		t.Errorf("thread PR state %q after refused merges, want it not merged", got)
	}
}
