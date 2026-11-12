package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Contract: memory-ticket-contract.md.

const _ticketBody = "SQLite round 1 call from Bhanu: %s for the journal."

// merged1 is the wake of PR #1 merging into the default branch at its fake head.
var merged1 = "PR #1 merged into main at " + headSHA(1)[:7]

// remember seeds a repo item with the SQLite words every memory check message matches. An empty
// ticket saves an item about no ticket.
func (f *prWakeFixture) remember(t *testing.T, call, ticket string) memoryItem {
	t.Helper()
	req := map[string]string{"scope": "repo", "scope_id": f.repo.ID, "kind": "decision", "body": fmt.Sprintf(_ticketBody, call), "why": "Queue state."}
	if ticket != "" {
		req["ticket"] = ticket
	}
	var item memoryItem
	f.s.do(t, "POST", "/api/memory", req, &item, 201)
	if item.ID == "" || item.Status != "accepted" || item.Ticket != ticket || item.RetiredReason != "" {
		t.Fatalf("saved item %+v, want accepted with ticket %q and no retired reason", item, ticket)
	}
	return item
}

func (f *prWakeFixture) item(t *testing.T, id string) memoryItem {
	t.Helper()
	for _, it := range memoryList(t, f.s) {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("memory item %s is not in the list", id)
	return memoryItem{}
}

func (f *prWakeFixture) wantRetired(t *testing.T, want memoryItem, reason string) {
	t.Helper()
	got := f.item(t, want.ID)
	if got.Status != "retired" || got.RetiredReason != reason || got.Body != want.Body || got.Why != want.Why || got.Ticket != want.Ticket {
		t.Errorf("item after the issue closed: %+v, want retired with reason %q, same body, why and ticket as %+v", got, reason, want)
	}
}

// firstPrompt starts a thread in the repo, sends the memory check message and returns the first
// message the provider got.
func (f *prWakeFixture) firstPrompt(t *testing.T) string {
	t.Helper()
	th := f.thread(t, f.boss, f.repo)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, _checkMemory)
	until(t, ch, r.ID, "run_finished")
	if done := f.s.run(t, r.ID); done.Status != "done" {
		t.Fatalf("memory check run: %+v", done)
	}
	return lastPrompt(t, f.m, f.provider, "user")
}

func (f *prWakeFixture) wantInjected(t *testing.T, prompt string, injected bool, items ...memoryItem) {
	t.Helper()
	for _, it := range items {
		if got := strings.Contains(prompt, "["+it.ID+"]"); got != injected {
			t.Errorf("item %s (%s) in the first message: %v, want %v\n%s", it.ID, it.Ticket, got, injected, prompt)
		}
	}
}

// Reef reads each ticket's issue with an ETag and retires the accepted items of a closed one, for
// either close reason; nothing else changes, no read is repeated for a ticket with no accepted
// item, and no thread or pull request is needed.
func TestMemoryTicket_ClosedIssueRetiresItems(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			a := f.remember(t, "design A", "#7")
			b := f.remember(t, "queue order", "#7")
			c := f.remember(t, "design B", "#8")
			d := f.remember(t, "no ticket", "")
			e := f.remember(t, "not planned", "#9")
			f.wantInjected(t, f.firstPrompt(t), true, a, b, c, d, e)

			eventually(t, 30*time.Second, "the issue is read again and answered 304", func() bool {
				_, notModified := f.gh.issueReads(7)
				return notModified >= 2
			})
			for _, it := range []memoryItem{a, b, c, d, e} {
				if got := f.item(t, it.ID); got.Status != "accepted" {
					t.Fatalf("item %+v retired while its issue is open", got)
				}
			}

			f.gh.setIssue(7, "closed", "completed")
			f.gh.setIssue(9, "closed", "not_planned")
			eventually(t, 30*time.Second, "the items of closed issues are retired", func() bool {
				return f.item(t, a.ID).Status == "retired" && f.item(t, b.ID).Status == "retired" && f.item(t, e.ID).Status == "retired"
			})
			f.wantRetired(t, a, "issue closed")
			f.wantRetired(t, b, "issue closed")
			f.wantRetired(t, e, "issue closed")
			for _, it := range []memoryItem{c, d} {
				if got := f.item(t, it.ID); got.Status != "accepted" || got.RetiredReason != "" {
					t.Errorf("item %+v changed, but its issue is open or it has none", got)
				}
			}

			f.wantInjected(t, f.firstPrompt(t), false, a, b, e)
			f.wantInjected(t, f.firstPrompt(t), true, c, d)

			reads7, _ := f.gh.issueReads(7)
			reads8, _ := f.gh.issueReads(8)
			time.Sleep(time.Second)
			if now, _ := f.gh.issueReads(7); now != reads7 {
				t.Errorf("issue #7 read %d more times after its items were retired, want none", now-reads7)
			}
			if now, _ := f.gh.issueReads(8); now <= reads8 {
				t.Errorf("issue #8, still open with an accepted item, was not read again")
			}

			f.s.do(t, "POST", "/api/memory/"+d.ID+"/retire", nil, nil, 204)
			if got := f.item(t, d.ID); got.Status != "retired" || got.RetiredReason != "" {
				t.Errorf("item retired by the user: %+v, want retired with no reason", got)
			}
		})
	}
}

// A ticket is `#` and digits, only on a repo item.
func TestMemoryTicket_OnlyOnRepoItems(t *testing.T) {
	f := newPRWakeFixture(t, _providers[0])
	post := func(scope, scopeID, ticket string, want int) {
		t.Helper()
		f.s.do(t, "POST", "/api/memory", map[string]string{"scope": scope, "scope_id": scopeID, "kind": "decision",
			"body": "SQLite " + ticket, "why": "Because.", "ticket": ticket}, nil, want)
	}
	post("repo", f.repo.ID, "#7", http.StatusCreated)
	post("repo", f.repo.ID, "#1234", http.StatusCreated)
	for _, bad := range []string{"7", "#", "#0", "#07", "#7a", "# 7", "#7 ", "#-1", "x#7", "#7\n"} {
		post("repo", f.repo.ID, bad, http.StatusBadRequest)
	}
	post("user", "", "#7", http.StatusBadRequest)
	post("bot", f.boss.ID, "#7", http.StatusBadRequest)
	if got := len(memoryList(t, f.s)); got != 2 {
		t.Errorf("%d items saved, want only the 2 valid ones", got)
	}
}

// The wake of a merged PR names the accepted items about the tickets its body closes, by id, with a
// clause per ticket; Refs, another ticket, a retired item and an item with no ticket are not named.
func TestMemoryTicket_MergedWakeNamesItems(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			x := f.remember(t, "design A", "#7")
			y := f.remember(t, "queue order", "#7")
			z := f.remember(t, "fixed on the way", "#9")
			refs := f.remember(t, "only referenced", "#8")
			none := f.remember(t, "no ticket", "")
			gone := f.remember(t, "already retired", "#7")
			f.s.do(t, "POST", "/api/memory/"+gone.ID+"/retire", nil, nil, 204)
			th := f.openPR(t, 1)
			first := f.s.thread(t, th.ID).RunID
			f.gh.setPullBody(1, "Stops the bug.\n\nThis resolves #7, and fixes #9 on the way.\n\nRefs #8\n")

			f.gh.setPull(1, "closed", true)
			w := f.waitWake(t, th, 1)

			f.wantWake(t, th, w, "merged", merged1+". Memory about #7 to replace or retire: ")
			f.wantReply(t, th, "Merge noted.", first)
			for _, want := range []string{"Memory about #9 to replace or retire: " + z.ID, x.ID, y.ID} {
				if !strings.Contains(w.Text, want) {
					t.Errorf("wake %q lacks %q", w.Text, want)
				}
			}
			for _, notWant := range []string{refs.ID, none.ID, gone.ID, "#8", "SQLite", "design A"} {
				if strings.Contains(w.Text, notWant) {
					t.Errorf("wake %q names %q, want only the accepted items of #7 and #9, by id", w.Text, notWant)
				}
			}
		})
	}
}

// A PR that only refers to a ticket, or one whose tickets have no accepted item, wakes with the plain
// sentence.
func TestMemoryTicket_MergedWakeWithoutItemsIsPlain(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			f.remember(t, "only referenced", "#8")
			th := f.openPR(t, 1)
			first := f.s.thread(t, th.ID).RunID
			f.gh.setPullBody(1, "Test only.\n\nRefs #8\nCloses #12\n")

			f.gh.setPull(1, "closed", true)
			w := f.waitWake(t, th, 1)

			f.wantWake(t, th, w, "merged", merged1)
			f.wantReply(t, th, "Merge noted.", first)
			if w.Text != merged1 {
				t.Errorf("wake %q, want exactly %q", w.Text, merged1)
			}
		})
	}
}

// GitHub closes the issue with the merge. Reef reads the PR before the issue in a tick, so the wake
// still names the items, which are retired right after.
func TestMemoryTicket_MergeThatClosesTheIssueWakesBeforeRetiring(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			x := f.remember(t, "design A", "#7")
			th := f.openPR(t, 1)
			first := f.s.thread(t, th.ID).RunID
			f.gh.setPullBody(1, "Issue: Closes #7")

			f.gh.mergeClosing(1, 7)
			w := f.waitWake(t, th, 1)

			f.wantWake(t, th, w, "merged", merged1+". Memory about #7 to replace or retire: "+x.ID)
			f.wantReply(t, th, "Merge noted.", first)
			eventually(t, 30*time.Second, "the item of the closed issue is retired", func() bool { return f.item(t, x.ID).Status == "retired" })
			f.wantRetired(t, x, "issue closed")
		})
	}
}
