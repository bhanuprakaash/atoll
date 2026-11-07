package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: helper-work-contract.md.

const _notIncluded = "Uncommitted changes in your worktree are not included."

// handoff is a delegate result.
type handoff struct {
	ThreadID string `json:"thread_id"`
	Branch   string `json:"branch"`
	Note     string `json:"note"`
}

func handedOff(t *testing.T, line string) handoff {
	t.Helper()
	text, _ := strings.CutPrefix(line, "atoll/delegate: ")
	var h handoff
	if json.Unmarshal([]byte(text), &h) != nil || h.ThreadID == "" {
		t.Fatalf("delegate result %q, want JSON with thread_id", line)
	}
	return h
}

// worktree is a start_work result.
type worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// startedWork reads the start_work result out of a reply: an mcp-use line
// "atoll/start_work: {…}", or a repo scenario's "start_work result: {…}".
func startedWork(t *testing.T, reply string) worktree {
	t.Helper()
	for _, l := range strings.Split(reply, "\n") {
		_, after, ok := strings.Cut(l, "start_work")
		var w worktree
		if i := strings.Index(after, "{"); ok && i >= 0 && json.Unmarshal([]byte(after[i:]), &w) == nil && w.Path != "" {
			return w
		}
	}
	t.Fatalf("reply %q holds no start_work result with a path", reply)
	return worktree{}
}

// bossWorktree has the boss call start_work, then the other calls, in one run, and returns its
// worktree.
func bossWorktree(t *testing.T, s *server, th thread, ev <-chan event, more ...fake.MCPCall) worktree {
	t.Helper()
	start := fake.MCPCall{Server: "atoll", Tool: "start_work", Args: json.RawMessage(`{"type":"fix","slug":"Seeded BUG"}`)}
	return startedWork(t, useAtoll(t, s, th, ev, append([]fake.MCPCall{start}, more...)...)[0])
}

// worktreeOf is the path of the repo's worktree on branch, or "" when it has none.
func worktreeOf(t *testing.T, repoPath, branch string) string {
	t.Helper()
	path := ""
	for _, l := range strings.Split(gitIn(t, repoPath, "worktree", "list", "--porcelain"), "\n") {
		if p, ok := strings.CutPrefix(l, "worktree "); ok {
			path = p
		}
		if l == "branch refs/heads/"+branch {
			return path
		}
	}
	return ""
}

func underRoot(t *testing.T, root, path string) {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsAbs(path) || rel == "." || strings.HasPrefix(rel, "..") {
		t.Errorf("worktree %q is not under the worktree root %q", path, root)
	}
}

func firstUserMessage(t *testing.T, s *server, threadID string) string {
	t.Helper()
	for _, msg := range messages(t, s, threadID) {
		if msg.Role == "user" {
			return msg.Text
		}
	}
	t.Fatalf("thread %s has no user message", threadID)
	return ""
}

// TestHelperWork_BranchesOffTheBoss: a boss with a worktree hands off; delegate makes the helper's
// worktree at once on <boss-branch>--tester from the boss's last commit, without its uncommitted
// changes, and says so. The helper is told to commit there and not push, its start_work returns
// that worktree, the message that wakes the boss names the branch to merge, and the boss's
// git merge brings the helper's commit in.
func TestHelperWork_BranchesOffTheBoss(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			s := f.s
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := f.thread(t, poteto)
			ev := s.events(t, boss)

			own := bossWorktree(t, s, boss, ev)
			writeFile(t, filepath.Join(own.Path, "boss.txt"), "committed\n")
			gitIn(t, own.Path, "add", "boss.txt")
			gitIn(t, own.Path, "commit", "-m", "boss work")
			head := gitIn(t, own.Path, "rev-parse", "HEAD")
			writeFile(t, filepath.Join(own.Path, "boss.txt"), "uncommitted\n")
			writeFile(t, filepath.Join(own.Path, "draft.txt"), "uncommitted\n")

			h := handedOff(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _unfinishedRepoPrompt, ""))[0])
			branch := own.Branch + "--tester"
			if h.Branch != branch || h.Note != _notIncluded {
				t.Errorf("delegate result %+v, want branch %q and note %q", h, branch, _notIncluded)
			}
			made := worktreeOf(t, f.repo.Path, branch)
			if tip := gitIn(t, f.repo.Path, "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch); tip != head || made == "" {
				t.Fatalf("when delegate returned: branch %s at %q with worktree %q, want a worktree on it at the boss's last commit %s", branch, tip, made, head)
			}
			underRoot(t, f.root, made)

			want := "Work on branch " + branch + " and commit there. Do not push or open a PR: Poteto merges it."
			first := firstUserMessage(t, s, h.ThreadID)
			if !strings.HasPrefix(first, "Poteto delegated this to you; your replies go to Poteto.\n") || !strings.HasSuffix(first, _unfinishedRepoPrompt) ||
				!strings.Contains("\n"+first+"\n", "\n"+want+"\n") || strings.Index(first, want) > strings.LastIndex(first, _unfinishedRepoPrompt) {
				t.Errorf("helper's first message %q, want the handoff line, then the line %q, then the brief", first, want)
			}

			helper := settled(t, s, h.ThreadID)
			if helper.Status != "done" {
				t.Fatalf("helper run: status %q, error %q; want done", helper.Status, helper.Error)
			}
			if got := startedWork(t, helper.Reply); got.Path != made || got.Branch != branch {
				t.Errorf("helper's start_work returned %+v, want the worktree delegate made: %s on %s", got, made, branch)
			}
			if b, _ := os.ReadFile(filepath.Join(made, "boss.txt")); string(b) != "committed\n" {
				t.Errorf("helper's boss.txt %q, want the boss's committed %q", b, "committed\n")
			}
			if _, err := os.Stat(filepath.Join(made, "draft.txt")); !os.IsNotExist(err) {
				t.Errorf("the boss's uncommitted draft.txt is in the helper's worktree (stat: %v)", err)
			}

			msg, _, _ := woken(t, f.m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "message from Tester", msg.Text, "Merge "+branch+" into your branch.")

			// The helper's commit: the scenario leaves its edits for the test to commit.
			gitIn(t, made, "add", "-A")
			gitIn(t, made, "commit", "-m", "fix: correct seeded answer")
			gitIn(t, own.Path, "merge", branch)
			for name, want := range map[string]string{"answer.txt": "42", "unfinished.txt": "keep this untracked work", "boss.txt": "committed"} {
				if got := gitIn(t, own.Path, "show", "HEAD:"+name); got != want {
					t.Errorf("boss's branch after git merge %s: %s is %q, want %q", branch, name, got, want)
				}
			}
		})
	}
}

// TestHelperWork_GitHubConnectionOnlyOwnBranch: on a GitHub repo, a helper without the GitHub
// connection gets its worktree off a boss branch, but its start_work for a branch of its own (boss
// with no worktree) is refused as today.
func TestHelperWork_GitHubConnectionOnlyOwnBranch(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubRepoFixture(t, p)
			repo := f.register(t, "octo-hello", f.hello(t))
			var poteto bot
			f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Poteto", "persona": "Works on GitHub.", "provider": p, "connections": []string{f.connect(t)}}, &poteto, http.StatusCreated)
			tester := f.s.newBot(t, "Tester", p)
			setDelegates(t, f.s, poteto, tester)

			boss := f.thread(t, poteto, repo)
			ev := f.s.events(t, boss)
			own := bossWorktree(t, f.s, boss, ev)
			h := handedOff(t, useAtoll(t, f.s, boss, ev, delegateCall("Tester", _unfinishedRepoPrompt, ""))[0])
			helper := settled(t, f.s, h.ThreadID)
			if helper.Status != "done" {
				t.Fatalf("helper without the GitHub connection on the boss's branch: status %q, error %q; want done", helper.Status, helper.Error)
			}
			if got := startedWork(t, helper.Reply); got.Branch != own.Branch+"--tester" {
				t.Errorf("helper's start_work returned %+v, want branch %s", got, own.Branch+"--tester")
			}

			exploring := f.thread(t, poteto, repo)
			h = handedOff(t, useAtoll(t, f.s, exploring, f.s.events(t, exploring), delegateCall("Tester", _githubFixPrompt, ""))[0])
			if h.Branch != "" || h.Note != "" {
				t.Errorf("delegate from a boss with no worktree: %+v, want no branch and no note", h)
			}
			helper = settled(t, f.s, h.ThreadID)
			if helper.Status != "done" || !strings.Contains(helper.Reply, "start_work failed: ") || !strings.Contains(helper.Reply, "give this Bot the GitHub connection") {
				t.Errorf("helper without the GitHub connection, boss with no worktree: status %q, reply %q, error %q; want start_work refused with %q",
					helper.Status, helper.Reply, helper.Error, "give this Bot the GitHub connection")
			}
		})
	}
}

// TestHelperWork_NoBossBranch: a boss thread on a repo that never called start_work, and a boss
// thread with no repo that names one (as Chief does), hand off with no branch: the helper's
// start_work makes its own <type>/<slug> branch from origin/HEAD, and it pushes and tracks its MR.
func TestHelperWork_NoBossBranch(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			s := f.s
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			base := gitIn(t, f.repo.Path, "rev-parse", "main")

			exploring := f.thread(t, poteto)
			h := handedOff(t, useAtoll(t, s, exploring, s.events(t, exploring), delegateCall("Tester", _fixRepoPrompt, ""))[0])
			if h.Branch != "" || h.Note != "" {
				t.Errorf("delegate from a repo thread with no worktree: %+v, want no branch and no note", h)
			}
			if first := firstUserMessage(t, s, h.ThreadID); strings.Contains(first, "Do not push") {
				t.Errorf("helper of a boss with no worktree is told %q, want it to push and open its own MR", first)
			}
			helper := settled(t, s, h.ThreadID)
			if helper.Status != "done" || !strings.Contains(helper.Reply, fake.MRURL) {
				t.Fatalf("helper run: status %q, reply %q, error %q; want done with the MR URL", helper.Status, helper.Reply, helper.Error)
			}
			w := startedWork(t, helper.Reply)
			if w.Branch != "fix/seeded-bug" {
				t.Errorf("helper's start_work branch %q, want fix/seeded-bug", w.Branch)
			}
			underRoot(t, f.root, w.Path)
			if got := gitIn(t, f.repo.Path, "rev-parse", "fix/seeded-bug~1"); got != base {
				t.Errorf("helper's branch starts at %s, want origin/HEAD %s", got, base)
			}
			if got := gitIn(t, f.repo.Path, "--git-dir", f.origin, "show", "refs/heads/fix/seeded-bug:answer.txt"); got != "42" {
				t.Errorf("origin's fix/seeded-bug answer.txt %q, want the pushed 42", got)
			}
			var mr struct {
				MRURL   string `json:"mr_url"`
				MRState string `json:"mr_state"`
			}
			s.do(t, "GET", "/api/threads/"+h.ThreadID, nil, &mr, http.StatusOK)
			if mr.MRURL != fake.MRURL || mr.MRState != "opened" {
				t.Errorf("helper thread MR %+v, want %s opened", mr, fake.MRURL)
			}

			chief := s.newThread(t, poteto)
			h = handedOff(t, useAtoll(t, s, chief, s.events(t, chief), delegateCall("Tester", _unfinishedRepoPrompt, "seeded"))[0])
			if h.Branch != "" || h.Note != "" {
				t.Errorf("delegate from a thread with no repo, repo seeded named: %+v, want no branch and no note", h)
			}
			helper = settled(t, s, h.ThreadID)
			if helper.Status != "done" {
				t.Fatalf("helper run: status %q, error %q; want done", helper.Status, helper.Error)
			}
			w = startedWork(t, helper.Reply)
			if w.Branch != "fix/seeded-bug-2" {
				t.Errorf("helper's start_work branch %q, want fix/seeded-bug-2", w.Branch)
			}
			underRoot(t, f.root, w.Path)
			if got := gitIn(t, f.repo.Path, "rev-parse", w.Branch); got != base {
				t.Errorf("helper's branch %s at %s, want origin/HEAD %s", w.Branch, got, base)
			}
		})
	}
}

// TestHelperWork_CloseOrMergeRemovesHelpers: closing the boss's thread, and the boss's MR merging,
// remove its helper's worktree after saving its unfinished work.
func TestHelperWork_CloseOrMergeRemovesHelpers(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			s := f.s
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)

			// helping hands off to a helper that leaves unfinished work in its worktree, waits out
			// the boss's wake and returns the helper thread, its worktree and its run.
			helping := func(boss thread, ev <-chan event) (thread, worktree, string) {
				t.Helper()
				h := handedOff(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _unfinishedRepoPrompt, ""))[0])
				r := settled(t, s, h.ThreadID)
				if r.Status != "done" {
					t.Fatalf("helper run: status %q, error %q; want done", r.Status, r.Error)
				}
				w := startedWork(t, r.Reply)
				if gitIn(t, w.Path, "status", "--porcelain") == "" {
					t.Fatalf("helper left no unfinished work in %s", w.Path)
				}
				quietBoss(t, s, boss.ID, tester.ID, 1)
				return thread{ID: h.ThreadID, BotID: tester.ID}, w, r.ID
			}
			removed := func(helper thread, w worktree, runID, reason string, ch <-chan event) {
				t.Helper()
				e := repoEvent(t, until(t, ch, runID, "worktree_removed"), "worktree_removed")
				ref := "refs/atoll/unfinished/" + helper.ID
				if e.Data["path"] != w.Path || e.Data["reason"] != reason || e.Data["snapshot_ref"] != ref {
					t.Errorf("helper's worktree_removed %v, want path %s, reason %s, snapshot_ref %s", e.Data, w.Path, reason, ref)
				}
				if _, err := os.Stat(w.Path); !os.IsNotExist(err) {
					t.Errorf("helper's worktree remains at %s: %v", w.Path, err)
				}
				for name, want := range map[string]string{"answer.txt": "42", "unfinished.txt": "keep this untracked work"} {
					if got := gitIn(t, f.repo.Path, "show", ref+":"+name); got != want {
						t.Errorf("saved %s = %q, want %q", name, got, want)
					}
				}
			}

			closing := f.thread(t, poteto)
			ev := s.events(t, closing)
			bossWorktree(t, s, closing, ev)
			helper, w, run := helping(closing, ev)
			hev := s.events(t, helper)
			s.do(t, "POST", "/api/threads/"+closing.ID+"/close", nil, nil, http.StatusNoContent)
			removed(helper, w, run, "closed", hev)

			merging := f.thread(t, poteto)
			ev = s.events(t, merging)
			own := bossWorktree(t, s, merging, ev, fake.MCPCall{Server: "atoll", Tool: "track_mr", Args: json.RawMessage(`{"url":"` + fake.MRURL + `"}`)})
			helper, w, run = helping(merging, ev)
			hev = s.events(t, helper)
			latest := getThread(t, s, merging.ID).RunID
			// ev still holds the boss wake run's run_finished; a fresh stream starts after it.
			ev = s.events(t, merging)
			writeFile(t, filepath.Join(f.m.bin, "mr-state"), "merged")
			assertRemoved(t, ev, latest, own.Path, "merged")
			removed(helper, w, run, "merged", hev)
		})
	}
}
