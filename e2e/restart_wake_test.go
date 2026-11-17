package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Restart wake (docs/design.md: Threads and runs, Delegation, App and UI). A run left going when
// the server starts is failed with `Reef restarted` and not resumed; the boss of the helpers it cut
// short gets one message with each helper's progress.

const (
	_restarted  = "Reef restarted"
	_oldWording = "Atoll stopped while the run was going"
	// _hangSession is the session of the run-hang recording: the helper's, which a `continue` resumes.
	_hangSession = "5a14034e-0f32-46ec-8e71-6dcf5e32cf31"
	_continueAll = "Continue a helper to pick up where it stopped."
	_continueOne = "Continue it to pick up where it stopped."
)

// restartedRun is a run as the API stores it.
type restartedRun struct {
	Status    string `json:"status"`
	Error     string `json:"error"`
	ErrorKind string `json:"error_kind"`
}

func runOf(t *testing.T, s *server, id string) restartedRun {
	t.Helper()
	var r restartedRun
	s.do(t, "GET", "/api/runs/"+id, nil, &r, http.StatusOK)
	return r
}

// apiBodies are the raw bodies a person's screens read for a thread.
func apiBodies(t *testing.T, s *server, botID, threadID, runID string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, path := range map[string]string{
		"run":      "/api/runs/" + runID,
		"thread":   "/api/threads/" + threadID,
		"messages": "/api/threads/" + threadID + "/messages",
		"threads":  "/api/bots/" + botID + "/threads",
		"bots":     "/api/bots",
	} {
		var raw json.RawMessage
		s.do(t, "GET", path, nil, &raw, http.StatusOK)
		out[name] = string(raw)
	}
	return out
}

// noOldWording fails when a prompt the provider got holds the old reason.
func noOldWording(t *testing.T, m *machine, p string) {
	t.Helper()
	for _, r := range promptLog(t, m, p) {
		if strings.Contains(r.Text, _oldWording) {
			t.Errorf("a %s prompt of kind %q holds %q: %q", p, r.Kind, _oldWording, r.Text)
		}
	}
}

// promptCount counts the prompts of the user kind the provider got: runs, not its probes or titles.
func promptCount(t *testing.T, m *machine, p string) int {
	t.Helper()
	n := 0
	for _, r := range promptLog(t, m, p) {
		if r.Kind == "user" {
			n++
		}
	}
	return n
}

// TestRestartWake_FailsGoingRunsAndDoesNotResume: a run `running` and a run `waiting` when the
// server starts are stored `failed`, error `Reef restarted`, error_kind `restarted`, and the
// provider is not started again for either.
func TestRestartWake_FailsGoingRunsAndDoesNotResume(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			going, asking := s.newThread(t, b), s.newThread(t, b)
			ev2 := s.events(t, asking)
			hung := s.send(t, going, _hangPrompt)
			waiting := s.send(t, asking, _writePrompt)
			helperRunning(t, s, going.ID)
			until(t, ev2, waiting.ID, "approval")
			if got := runOf(t, s, hung.ID).Status; got != "running" {
				t.Fatalf("run %s before the restart: status %q, want running", hung.ID, got)
			}
			if got := runOf(t, s, waiting.ID).Status; got != "waiting" {
				t.Fatalf("run %s before the restart: status %q, want waiting", waiting.ID, got)
			}

			s.kill()
			time.Sleep(2 * _pace)
			before := promptCount(t, m, p)
			s = m.serve(t)

			for _, id := range []string{hung.ID, waiting.ID} {
				if got := runOf(t, s, id); got.Status != "failed" || got.Error != _restarted || got.ErrorKind != "restarted" {
					t.Errorf("run %s after a restart: %+v, want status failed, error %q, error_kind restarted", id, got, _restarted)
				}
			}
			time.Sleep(2 * _chance)
			if after := promptCount(t, m, p); after != before {
				t.Errorf("the provider got %d prompts after the restart, want none: Reef does not resume a run", after-before)
			}
			for _, th := range []thread{going, asking} {
				got := getThread(t, s, th.ID)
				want := map[string]string{going.ID: hung.ID, asking.ID: waiting.ID}[th.ID]
				if got.RunID != want || got.RunStatus != "failed" {
					t.Errorf("thread %s after a restart: run %s %q, want run %s still, failed", th.ID, got.RunID, got.RunStatus, want)
				}
			}
		})
	}
}

// TestRestartWake_PersonThreadReadsReefRestarted: the thread of a person's own message, cut short by
// a restart, reads `Reef restarted` in every place the UI reads it, and the old wording is in none.
func TestRestartWake_PersonThreadReadsReefRestarted(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			r := s.send(t, th, _hangPrompt)
			helperRunning(t, s, th.ID)

			s.kill()
			s = m.serve(t)

			got := getThread(t, s, th.ID)
			var info struct {
				RunErrorKind string `json:"run_error_kind"`
			}
			s.do(t, "GET", "/api/threads/"+th.ID, nil, &info, http.StatusOK)
			if got.RunStatus != "failed" || info.RunErrorKind != "restarted" {
				t.Errorf("thread after a restart: run_status %q, run_error_kind %q; want failed, restarted (the UI words it Run failed: Reef restarted.)", got.RunStatus, info.RunErrorKind)
			}
			bodies := apiBodies(t, s, b.ID, th.ID, r.ID)
			for name, body := range bodies {
				if strings.Contains(body, _oldWording) {
					t.Errorf("GET %s holds %q: %s", name, _oldWording, body)
				}
			}
			if body := bodies["run"]; !strings.Contains(body, `"error":"`+_restarted+`"`) {
				t.Errorf("GET run %s = %s, want error %q", r.ID, body, _restarted)
			}
			// The same words reach a Bot when a later turn resumes the thread.
			ev := s.events(t, th)
			s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
			noOldWording(t, m, p)
		})
	}
}

// progress is the line a helper's block carries once it has commits ahead and uncommitted changes.
func progress(branch string, ahead int, threadID string) string {
	return fmt.Sprintf("Branch %s, %d commits ahead, uncommitted changes. (thread %s)", branch, ahead, threadID)
}

// cutHelpers is a boss with a worktree and one helper per bot, each hung in its own worktree with
// the commits and the uncommitted edit the test made there.
type cutHelpers struct {
	m       *machine
	s       *server
	f       *repoFixture
	boss    thread
	bossRun string
	main    string // the boss's branch
	ids     []string
	runs    []string
	paths   []string
	branch  []string
}

// newCutHelpers delegates one hung brief to each of names, then commits ahead[i] files in helper
// i's worktree and leaves an edit to a tracked file uncommitted.
func newCutHelpers(t *testing.T, p string, names []string, ahead []int) *cutHelpers {
	t.Helper()
	f := newRepoFixture(t, p)
	s := f.s
	poteto := s.newBot(t, "Poteto", p)
	var helpers []bot
	for _, n := range names {
		helpers = append(helpers, s.newBot(t, n, p))
	}
	setDelegates(t, s, poteto, helpers...)
	boss := f.thread(t, poteto)
	ev := s.events(t, boss)
	own := bossWorktree(t, s, boss, ev)
	c := &cutHelpers{m: f.m, s: s, f: f, boss: boss, main: own.Branch}
	for _, n := range names {
		h := handedOff(t, useAtoll(t, s, boss, ev, delegateCall(n, _hangPrompt, ""))[0])
		c.ids = append(c.ids, h.ThreadID)
		c.branch = append(c.branch, h.Branch)
		c.runs = append(c.runs, helperRunning(t, s, h.ThreadID))
	}
	for i := range names {
		path := worktreeOf(t, f.repo.Path, c.branch[i])
		if path == "" {
			t.Fatalf("helper %s has no worktree on %s", names[i], c.branch[i])
		}
		c.paths = append(c.paths, path)
		for n := 0; n < ahead[i]; n++ {
			file := fmt.Sprintf("work%d.txt", n)
			writeFile(t, filepath.Join(path, file), "done\n")
			gitIn(t, path, "add", file)
			gitIn(t, path, "commit", "-m", "helper work "+file)
		}
		writeFile(t, filepath.Join(path, "answer.txt"), "42 unfinished\n")
	}
	c.bossRun = getThread(t, s, boss.ID).RunID
	return c
}

func (c *cutHelpers) restart(t *testing.T) {
	t.Helper()
	c.s.kill()
	c.s = c.m.serve(t)
}

// restartMessages are the messages of the boss thread that report the restart.
func restartMessages(t *testing.T, s *server, boss string) []bossMessage {
	t.Helper()
	var out []bossMessage
	for _, msg := range bossMessages(t, s, boss) {
		if msg.Role == "user" && strings.Contains(msg.Text, _restarted) {
			out = append(out, msg)
		}
	}
	return out
}

// restartText waits for the boss's run after the restart and returns the one message that reports
// it, without the `Reef: ` the prompt adds.
func (c *cutHelpers) restartText(t *testing.T) bossMessage {
	t.Helper()
	wake := nextBossRun(t, c.s, c.boss.ID, c.bossRun)
	if wake.Status != "done" {
		t.Errorf("boss run after the restart: status %q, error %q; want done", wake.Status, wake.Error)
	}
	msgs := restartMessages(t, c.s, c.boss.ID)
	if len(msgs) != 1 {
		t.Fatalf("%d messages in the boss thread report the restart, want one: %+v", len(msgs), msgs)
	}
	if msgs[0].RunID != wake.ID {
		t.Errorf("restart message belongs to run %q, want the boss's run %s", msgs[0].RunID, wake.ID)
	}
	msgs[0].Text = strings.TrimPrefix(msgs[0].Text, "Reef: ")
	return msgs[0]
}

// TestRestartWake_TwoHelpersOneMessage: a restart that interrupted 2 helpers of one boss posts one
// message from Reef: the count line, then a block per helper in the order the runs began, then the
// helpers line. Each block names the helper, its branch, commits ahead and uncommitted changes, and
// its thread id.
func TestRestartWake_TwoHelpersOneMessage(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			c := newCutHelpers(t, p, []string{"Tester", "Reviewer"}, []int{3, 2})
			c.restart(t)

			msg := c.restartText(t)
			want := strings.Join([]string{
				"A restart interrupted 2 helper runs. " + _continueAll,
				"Tester stopped: " + _restarted + ".\n" + progress(c.branch[0], 3, c.ids[0]),
				"Reviewer stopped: " + _restarted + ".\n" + progress(c.branch[1], 2, c.ids[1]),
				helpersLine(),
			}, "\n\n")
			if msg.Text != want {
				t.Errorf("restart message:\n%s\nwant:\n%s", msg.Text, want)
			}
			if msg.FromBotID != "" {
				t.Errorf("restart message from_bot_id %q, want Reef's (empty)", msg.FromBotID)
			}
			for i, id := range c.ids {
				if r := runOf(t, c.s, c.runs[i]); r.Status != "failed" || r.Error != _restarted || r.ErrorKind != "restarted" {
					t.Errorf("helper %s run after the restart: %+v, want failed, %q, restarted", id, r, _restarted)
				}
			}
			noOldWording(t, c.m, p)
			if strings.Contains(msg.Text, _oldWording) {
				t.Errorf("restart message holds %q", _oldWording)
			}
		})
	}
}

// TestRestartWake_OneHelperTwoLines: with one interrupted helper the message is that helper's two
// lines, `Continue it to pick up where it stopped.`, then the helpers line, from the helper's Bot.
func TestRestartWake_OneHelperTwoLines(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			c := newCutHelpers(t, p, []string{"Tester"}, []int{3})
			c.restart(t)

			msg := c.restartText(t)
			want := strings.Join([]string{
				"Tester stopped: " + _restarted + ".\n" + progress(c.branch[0], 3, c.ids[0]) + "\n" + _continueOne,
				helpersLine(),
			}, "\n\n")
			if msg.Text != want {
				t.Errorf("restart message:\n%s\nwant:\n%s", msg.Text, want)
			}
			if strings.Contains(msg.Text, "A restart interrupted") || strings.Contains(msg.Text, _oldWording) {
				t.Errorf("restart message %q, want the single-helper form with no count line and no old wording", msg.Text)
			}
			noOldWording(t, c.m, p)
		})
	}
}

// TestRestartWake_NoBranchYet: a helper that has no branch reads `No branch yet. (thread {id})`.
func TestRestartWake_NoBranchYet(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ids, bossRun := delegateTester(t, s, boss, s.events(t, boss), _hangPrompt)
			helperRunning(t, s, ids[0])

			s.kill()
			s = m.serve(t)

			wake := nextBossRun(t, s, boss.ID, bossRun)
			msgs := restartMessages(t, s, boss.ID)
			if len(msgs) != 1 || wake.Status != "done" {
				t.Fatalf("%d restart messages, boss run %q; want one, done: %+v", len(msgs), wake.Status, msgs)
			}
			want := "Tester stopped: " + _restarted + ".\nNo branch yet. (thread " + ids[0] + ")\n" + _continueOne + "\n\n" + helpersLine()
			if got := strings.TrimPrefix(msgs[0].Text, "Reef: "); got != want {
				t.Errorf("restart message:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestRestartWake_NotRepeated: the message is stored with the runs it reports, so a second restart,
// with nothing going, posts nothing and starts no boss run.
func TestRestartWake_NotRepeated(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			c := newCutHelpers(t, p, []string{"Tester", "Reviewer"}, []int{1, 2})
			c.restart(t)
			c.restartText(t)
			after := getThread(t, c.s, c.boss.ID)

			c.restart(t)
			time.Sleep(2 * _chance)

			if msgs := restartMessages(t, c.s, c.boss.ID); len(msgs) != 1 {
				t.Errorf("%d restart messages after a second restart, want the one: %+v", len(msgs), msgs)
			}
			if got := getThread(t, c.s, c.boss.ID); got.RunID != after.RunID || got.RunStatus != after.RunStatus {
				t.Errorf("boss thread after a second restart: run %s %q, want run %s %q, no new run", got.RunID, got.RunStatus, after.RunID, after.RunStatus)
			}
			for i, id := range c.ids {
				if got := getThread(t, c.s, id); got.RunID != c.runs[i] || got.RunStatus != "failed" {
					t.Errorf("helper thread %s after a second restart: run %s %q, want run %s still, failed", id, got.RunID, got.RunStatus, c.runs[i])
				}
			}
		})
	}
}

// TestRestartWake_ContinueResumesInItsWorktree: after the restart the boss's `continue` to the
// interrupted helper resumes the helper's own session in its own worktree: the same branch, its
// commits and its uncommitted edit still there.
func TestRestartWake_ContinueResumesInItsWorktree(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			c := newCutHelpers(t, p, []string{"Tester"}, []int{3})
			c.restart(t)
			c.restartText(t)
			s, helper, path, branch := c.s, c.ids[0], c.paths[0], c.branch[0]

			time.Sleep(_chance)
			if got := getThread(t, s, helper); got.RunID != c.runs[0] || got.RunStatus != "failed" {
				t.Fatalf("helper thread after the restart: run %s %q, want run %s failed and nothing resumed", got.RunID, got.RunStatus, c.runs[0])
			}

			got := useAtoll(t, s, c.boss, s.events(t, c.boss), continueCall(helper, _keepPrompt))
			if len(got) == 0 || got[0] != "atoll/continue: Sent." {
				t.Fatalf("continue to the interrupted helper: reply %q, want %q", got, "atoll/continue: Sent.")
			}
			eventually(t, 10*time.Second, "a new run of the helper", func() bool { return getThread(t, s, helper).RunID != c.runs[0] })
			if r := settled(t, s, helper); r.Status != "done" {
				t.Fatalf("helper's run for the continue: status %q, error %q; want done", r.Status, r.Error)
			}

			call := sessionWith(t, c.m, p, _keepPrompt)
			if call == nil {
				t.Fatalf("no %s session got the continue %q", p, _keepPrompt)
			}
			if got := resumed(call, p); got != _hangSession {
				t.Errorf("the continue resumed session %q, want the helper's own %q", got, _hangSession)
			}
			want, err := filepath.EvalSymlinks(path)
			if err != nil {
				t.Fatal(err)
			}
			if call.cwd != want {
				t.Errorf("the continue ran in %q, want the helper's worktree %q", call.cwd, want)
			}
			if got := gitIn(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
				t.Errorf("helper's worktree is on %q after the continue, want %q", got, branch)
			}
			if got := gitIn(t, path, "rev-list", "--count", c.main+".."+branch); got != "3" {
				t.Errorf("helper's branch is %s commits ahead after the continue, want 3", got)
			}
			if got := gitIn(t, path, "status", "--porcelain"); !strings.Contains(got, "answer.txt") {
				t.Errorf("git status in the helper's worktree after the continue = %q, want its uncommitted answer.txt", got)
			}
		})
	}
}

// quit stops the server the way quitting the app does, with SIGTERM, and serves it again.
func (c *cutHelpers) quit(t *testing.T) {
	t.Helper()
	if err := c.s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("atoll serve after SIGTERM: %v\nstderr:\n%s", err, c.s.log)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("atoll serve did not stop cleanly within 15s after SIGTERM")
	}
	c.s = c.m.serve(t)
}

// kindedMessages are the boss thread's messages with their kind.
func kindedMessages(t *testing.T, s *server, boss string) []struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	Kind      string `json:"kind"`
	FromBotID string `json:"from_bot_id"`
} {
	t.Helper()
	var out []struct {
		Role      string `json:"role"`
		Text      string `json:"text"`
		Kind      string `json:"kind"`
		FromBotID string `json:"from_bot_id"`
	}
	s.do(t, "GET", "/api/threads/"+boss+"/messages", nil, &out, http.StatusOK)
	return out
}

// TestRestartWake_GracefulQuitPostsOneMessage: quitting the app (SIGTERM) and starting it again is a
// restart like a crash: one restart message from Reef in the boss thread, the same as after a kill,
// and no message per helper in the old `{Helper}'s run failed:` form.
func TestRestartWake_GracefulQuitPostsOneMessage(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p+"/two helpers", func(t *testing.T) {
			c := newCutHelpers(t, p, []string{"Tester", "Reviewer"}, []int{3, 2})
			c.quit(t)

			msg := c.restartText(t)
			want := strings.Join([]string{
				"A restart interrupted 2 helper runs. " + _continueAll,
				"Tester stopped: " + _restarted + ".\n" + progress(c.branch[0], 3, c.ids[0]),
				"Reviewer stopped: " + _restarted + ".\n" + progress(c.branch[1], 2, c.ids[1]),
				helpersLine(),
			}, "\n\n")
			if msg.Text != want {
				t.Errorf("restart message:\n%s\nwant:\n%s", msg.Text, want)
			}
			if msg.FromBotID != "" {
				t.Errorf("restart message from_bot_id %q, want Reef's (empty)", msg.FromBotID)
			}
			c.onlyRestartMessage(t)
			for i, id := range c.ids {
				if r := runOf(t, c.s, c.runs[i]); r.Status != "failed" || r.Error != _restarted || r.ErrorKind != "restarted" {
					t.Errorf("helper %s run after the quit: %+v, want failed, %q, restarted", id, r, _restarted)
				}
			}
		})
		t.Run(p+"/one helper", func(t *testing.T) {
			c := newCutHelpers(t, p, []string{"Tester"}, []int{3})
			c.quit(t)

			msg := c.restartText(t)
			want := strings.Join([]string{
				"Tester stopped: " + _restarted + ".\n" + progress(c.branch[0], 3, c.ids[0]) + "\n" + _continueOne,
				helpersLine(),
			}, "\n\n")
			if msg.Text != want {
				t.Errorf("restart message:\n%s\nwant:\n%s", msg.Text, want)
			}
			c.onlyRestartMessage(t)
			if r := runOf(t, c.s, c.runs[0]); r.Status != "failed" || r.Error != _restarted || r.ErrorKind != "restarted" {
				t.Errorf("helper run after the quit: %+v, want failed, %q, restarted", r, _restarted)
			}
		})
	}
}

// onlyRestartMessage fails when the boss thread holds any message about a helper's run but the one
// restart message: the old form `{Helper}'s run failed:` is none of them. With 2 helpers the one is
// of kind restart; with 1 it is that helper's.
func (c *cutHelpers) onlyRestartMessage(t *testing.T) {
	t.Helper()
	var about []string
	for _, m := range kindedMessages(t, c.s, c.boss.ID) {
		if m.Role != "user" {
			continue
		}
		if strings.Contains(m.Text, "run failed:") || strings.Contains(m.Text, _oldWording) {
			t.Errorf("boss message %q is in the old form, want the restart message only", m.Text)
		}
		for _, id := range c.ids {
			if strings.Contains(m.Text, id) {
				about = append(about, m.Kind+": "+m.Text)
				break
			}
		}
	}
	if len(about) != 1 {
		t.Errorf("%d boss messages name a helper thread after the quit, want the one restart message: %q", len(about), about)
	}
	if len(c.ids) > 1 && len(about) == 1 && !strings.HasPrefix(about[0], "restart: ") {
		t.Errorf("restart message kind: %q, want restart", strings.SplitN(about[0], ":", 2)[0])
	}
}
