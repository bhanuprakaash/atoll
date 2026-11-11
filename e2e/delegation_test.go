package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"atoll/e2e/fakes/fake"
)

// Contract: delegation-contract.md.

const _helpersFull = "3 helpers running. Tell the user, and hand this off when one of them reports back."

type threadState struct {
	ID             string `json:"id"`
	BotID          string `json:"bot_id"`
	RepoID         string `json:"repo_id"`
	ParentThreadID string `json:"parent_thread_id"`
	RunID          string `json:"run_id"`
	RunStatus      string `json:"run_status"`
}

func getThread(t *testing.T, s *server, id string) threadState {
	t.Helper()
	var th threadState
	s.do(t, "GET", "/api/threads/"+id, nil, &th, http.StatusOK)
	return th
}

func setDelegates(t *testing.T, s *server, boss bot, helpers ...bot) {
	t.Helper()
	ids := []string{}
	for _, h := range helpers {
		ids = append(ids, h.ID)
	}
	s.do(t, "PUT", "/api/bots/"+boss.ID, map[string]any{"delegates": ids}, nil, http.StatusOK)
}

// escaped is s as a JSON string with every character \u-escaped. A brief holds the prompt of the
// recording the helper should play; escaped, the boss's own prompt does not hold it, so the fake
// plays mcp-use for the boss.
func escaped(s string) json.RawMessage {
	var b strings.Builder
	b.WriteByte('"')
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, `\u%04x`, u)
	}
	b.WriteByte('"')
	return json.RawMessage(b.String())
}

func delegateCall(bot, brief, repo string) fake.MCPCall {
	args := map[string]any{"bot": bot, "brief": escaped(brief)}
	if repo != "" {
		args["repo"] = repo
	}
	b, _ := json.Marshal(args)
	return fake.MCPCall{Server: "atoll", Tool: "delegate", Args: b}
}

func continueCall(thread, message string) fake.MCPCall {
	b, _ := json.Marshal(map[string]any{"thread": thread, "message": escaped(message)})
	return fake.MCPCall{Server: "atoll", Tool: "continue", Args: b}
}

// useAtoll has the thread's Bot make the calls in one run, which must end done, and returns the
// reply: one line per call.
func useAtoll(t *testing.T, s *server, th thread, ev <-chan event, calls ...fake.MCPCall) []string {
	t.Helper()
	payload, _ := json.Marshal(calls)
	r := s.finished(t, ev, s.send(t, th, _mcpPromptHead+string(payload)).ID)
	if r.Status != "done" {
		t.Fatalf("run %s: status %q, error %q; want done", r.ID, r.Status, r.Error)
	}
	return strings.Split(r.Reply, "\n")
}

// delegated reads the helper thread id out of a delegate result line.
func delegated(t *testing.T, line string) string {
	t.Helper()
	text, _ := strings.CutPrefix(line, "atoll/delegate: ")
	var out struct {
		ThreadID string `json:"thread_id"`
	}
	if json.Unmarshal([]byte(text), &out) != nil || out.ThreadID == "" {
		t.Fatalf("delegate result %q, want {\"thread_id\":\"<id>\"}", line)
	}
	return out.ThreadID
}

// settled waits until the thread's latest run is no longer going and returns it.
func settled(t *testing.T, s *server, id string) runInfo {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		th := getThread(t, s, id)
		if th.RunID != "" && th.RunStatus != "running" && th.RunStatus != "waiting" {
			return s.run(t, th.RunID)
		}
		if time.Now().After(deadline) {
			t.Fatalf("thread %s: latest run %q still %q after 30s", id, th.RunID, th.RunStatus)
		}
		time.Sleep(_pace)
	}
}

// helperSession waits for the provider session whose first message was a handoff.
func helperSession(t *testing.T, m *machine, p string) *call {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		for _, c := range m.sessions(t, p) {
			if slices.ContainsFunc(c.stdin, func(l string) bool { return strings.Contains(l, "delegated this to you") }) {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s session got a handoff message in 30s", p)
		}
		time.Sleep(_pace)
	}
}

// quietBoss waits out boss wakes: once n of its helpers' runs have ended, Reef may post n messages
// from the helper Bot into the boss thread, each starting a boss run, and a message to a thread
// with a going run is refused. It returns when the thread holds n such messages, or none came in
// 4*_pace (a server without wakes), and its latest run has ended and stayed latest for 2*_pace.
func quietBoss(t *testing.T, s *server, boss, helperBot string, n int) {
	t.Helper()
	start, deadline := time.Now(), time.Now().Add(30*time.Second)
	last, since := "", time.Now()
	for {
		woke := 0
		var msgs []struct {
			FromBotID string `json:"from_bot_id"`
		}
		s.do(t, "GET", "/api/threads/"+boss+"/messages", nil, &msgs, http.StatusOK)
		for _, msg := range msgs {
			if msg.FromBotID == helperBot {
				woke++
			}
		}
		th := getThread(t, s, boss)
		if th.RunID != last || th.RunStatus == "running" || th.RunStatus == "waiting" {
			last, since = th.RunID, time.Now()
		}
		if (woke >= n || woke == 0 && time.Since(start) > 4*_pace) && time.Since(since) > 2*_pace {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("boss thread %s: %d of %d messages from the helper Bot, latest run %s %q after 30s", boss, woke, n, th.RunID, th.RunStatus)
		}
		time.Sleep(_pace / 3)
	}
}

// TestDelegate_HandsOff: delegate returns {thread_id} at once, the boss's run ends done while the
// helper still works, and the helper thread has the boss thread as parent, the helper Bot, no
// repo, the handoff line and brief as its first message, and a fresh provider session.
func TestDelegate_HandsOff(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)

			lines := useAtoll(t, s, boss, s.events(t, boss), delegateCall("Tester", _hangPrompt, ""))
			if len(lines) != 1 {
				t.Fatalf("reply %q, want one delegate result", lines)
			}
			helper := getThread(t, s, delegated(t, lines[0]))
			if helper.ParentThreadID != boss.ID || helper.BotID != tester.ID || helper.RepoID != "" {
				t.Errorf("helper thread %+v, want parent_thread_id %s, bot_id %s (Tester), no repo_id", helper, boss.ID, tester.ID)
			}
			if helper.RunStatus != "running" {
				t.Errorf("helper's run %q is %q after the boss's run ended, want running: delegate must not wait for it", helper.RunID, helper.RunStatus)
			}

			var msgs []message
			s.do(t, "GET", "/api/threads/"+helper.ID+"/messages", nil, &msgs, http.StatusOK)
			want := "Poteto delegated this to you; your replies go to Poteto.\n\n" + _hangPrompt
			if i := slices.IndexFunc(msgs, func(x message) bool { return x.Role == "user" }); i < 0 || msgs[i].Text != want {
				t.Errorf("helper thread messages %+v, want the first user message %q", msgs, want)
			}

			c := helperSession(t, m, p)
			switch p {
			case "claude":
				if slices.Contains(c.argv, "--resume") {
					t.Errorf("helper's claude run resumed a session, want a fresh one; argv %q", c.argv)
				}
			case "codex":
				if request(c, "thread/start") == nil || request(c, "thread/resume") != nil {
					t.Errorf("helper's codex run did not start a fresh thread with thread/start; stdin:\n%s", strings.Join(c.stdin, "\n"))
				}
			}
		})
	}
}

// TestDelegate_ToolsOnlyForBosses: a Bot with no delegates, and a helper's run even when the
// helper Bot has delegates, see neither delegate nor continue. A Bot with delegates sees both.
func TestDelegate_ToolsOnlyForBosses(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester, reviewer, ada := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p), s.newBot(t, "Ada", p)
			setDelegates(t, s, poteto, tester)
			setDelegates(t, s, tester, reviewer)
			absent := []string{"atoll/delegate: unavailable", "atoll/continue: unavailable"}
			tryBoth := []fake.MCPCall{delegateCall("Reviewer", _keepPrompt, ""), continueCall("no-such-thread", _askPrompt)}

			plain := s.newThread(t, ada)
			if got := useAtoll(t, s, plain, s.events(t, plain), tryBoth...); !slices.Equal(got, absent) {
				t.Errorf("Ada (no delegates): reply %q, want %q", got, absent)
			}

			own := s.newThread(t, tester)
			if got := useAtoll(t, s, own, s.events(t, own), continueCall("no-such-thread", _askPrompt)); len(got) != 1 || got[0] == absent[1] {
				t.Errorf("Tester (delegates Reviewer) in its own thread: reply %q, want continue listed (a tool error for an unknown thread)", got)
			}

			boss := s.newThread(t, poteto)
			brief, _ := json.Marshal(tryBoth)
			lines := useAtoll(t, s, boss, s.events(t, boss), delegateCall("Tester", _mcpPromptHead+string(brief), ""))
			helper := settled(t, s, delegated(t, lines[0]))
			if helper.Status != "done" || helper.Reply != strings.Join(absent, "\n") {
				t.Errorf("Tester's helper run: status %q, reply %q, error %q; want done, %q", helper.Status, helper.Reply, helper.Error, strings.Join(absent, "\n"))
			}
		})
	}
}

// TestDelegate_Refused: a Bot not on the boss's list gets a tool error naming the list, and no
// thread is made for it; three helper runs going refuse a fourth.
func TestDelegate_Refused(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester, reviewer, mako := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p), s.newBot(t, "Mako", p)
			setDelegates(t, s, poteto, tester, reviewer)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev, delegateCall("Mako", _keepPrompt, ""))
			if len(lines) != 1 || !strings.HasPrefix(lines[0], "atoll/delegate: ") || lines[0] == "atoll/delegate: unavailable" ||
				!strings.Contains(lines[0], "Tester") || !strings.Contains(lines[0], "Reviewer") {
				t.Errorf("delegate to Mako: reply %q, want a tool error naming Tester and Reviewer", lines)
			}
			var threads []thread
			s.do(t, "GET", "/api/bots/"+mako.ID+"/threads", nil, &threads, http.StatusOK)
			if len(threads) != 0 {
				t.Errorf("Mako's threads %+v after a refused delegate, want none", threads)
			}

			hang := delegateCall("Tester", _hangPrompt, "")
			lines = useAtoll(t, s, boss, ev, hang, hang, hang, hang)
			if len(lines) != 4 {
				t.Fatalf("four delegates: reply %q, want four lines", lines)
			}
			ids := []string{}
			for _, l := range lines[:3] {
				ids = append(ids, delegated(t, l))
			}
			if slices.Sort(ids); len(slices.Compact(ids)) != 3 {
				t.Errorf("first three delegates gave threads %q, want three different ones", ids)
			}
			if want := "atoll/delegate: " + _helpersFull; lines[3] != want {
				t.Errorf("fourth delegate with three helpers running: %q, want %q", lines[3], want)
			}
		})
	}
}

// TestDelegate_HelperRepo: the helper thread gets the boss thread's repo; a boss thread with no
// repo may name one with repo, and without it the helper has none. A helper without access to
// the repo is refused with "Tester has no access to atoll".
func TestDelegate_HelperRepo(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			ids := registerRepos(t, s, "atoll", "reef")
			var repos []registeredRepo
			s.do(t, "GET", "/api/repos", nil, &repos, http.StatusOK)
			byName := map[string]string{}
			for _, r := range repos {
				byName[r.Name] = r.ID
			}
			if len(ids) != 2 || byName["atoll"] == "" || byName["reef"] == "" {
				t.Fatalf("GET /api/repos %+v, want atoll and reef", repos)
			}
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)

			var onRepo thread
			s.do(t, "POST", "/api/bots/"+poteto.ID+"/threads", map[string]string{"repo_id": byName["atoll"]}, &onRepo, http.StatusCreated)
			lines := useAtoll(t, s, onRepo, s.events(t, onRepo), delegateCall("Tester", _keepPrompt, ""))
			h := getThread(t, s, delegated(t, lines[0]))
			if h.RepoID != byName["atoll"] || h.ParentThreadID != onRepo.ID {
				t.Errorf("helper of a boss thread on atoll: %+v, want repo_id %s (atoll), parent %s", h, byName["atoll"], onRepo.ID)
			}
			settled(t, s, h.ID)

			noRepo := s.newThread(t, poteto)
			lines = useAtoll(t, s, noRepo, s.events(t, noRepo), delegateCall("Tester", _keepPrompt, "reef"), delegateCall("Tester", _keepPrompt, ""))
			if len(lines) != 2 {
				t.Fatalf("two delegates: reply %q, want two lines", lines)
			}
			for i, want := range []string{byName["reef"], ""} {
				h := getThread(t, s, delegated(t, lines[i]))
				if h.RepoID != want {
					t.Errorf("helper %d of a boss thread with no repo (repo %q named): %+v, want repo_id %q", i+1, []string{"reef", ""}[i], h, want)
				}
				settled(t, s, h.ID)
			}

			s.do(t, "PUT", "/api/bots/"+tester.ID, map[string]any{"repo_access": []string{}}, nil, http.StatusOK)
			// Fresh boss threads: the helpers above may still be waking onRepo and noRepo.
			const noAccess = "atoll/delegate: Tester has no access to atoll"
			s.do(t, "POST", "/api/bots/"+poteto.ID+"/threads", map[string]string{"repo_id": byName["atoll"]}, &onRepo, http.StatusCreated)
			if got := useAtoll(t, s, onRepo, s.events(t, onRepo), delegateCall("Tester", _keepPrompt, "")); !slices.Equal(got, []string{noAccess}) {
				t.Errorf("Tester without access, boss thread on atoll: reply %q, want %q", got, noAccess)
			}
			noRepo = s.newThread(t, poteto)
			if got := useAtoll(t, s, noRepo, s.events(t, noRepo), delegateCall("Tester", _keepPrompt, "atoll")); !slices.Equal(got, []string{noAccess}) {
				t.Errorf("Tester without access, repo atoll named: reply %q, want %q", got, noAccess)
			}
		})
	}
}

// TestContinue_OnlyFromBoss: you cannot post to a helper thread; continue to it is refused from
// another Bot with delegates and from another thread of the same boss, and accepted from the boss
// thread, which gives the helper a new run.
func TestContinue_OnlyFromBoss(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, mako, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Mako", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			setDelegates(t, s, mako, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _keepPrompt, ""))[0])
			first := settled(t, s, helper)

			var refused struct {
				Error string `json:"error"`
			}
			s.do(t, "POST", "/api/threads/"+helper+"/messages", map[string]string{"text": "Stop that."}, &refused, http.StatusConflict)
			if refused.Error != "Poteto is handling this thread" {
				t.Errorf("POST to the helper thread: error %q, want %q", refused.Error, "Poteto is handling this thread")
			}

			other, sibling := s.newThread(t, mako), s.newThread(t, poteto)
			var refusals []string
			for _, c := range []struct {
				who string
				th  thread
			}{{"Mako's thread", other}, {"another Poteto thread", sibling}} {
				lines := useAtoll(t, s, c.th, s.events(t, c.th), continueCall(helper, _askPrompt))
				if len(lines) != 1 || lines[0] == "atoll/continue: unavailable" {
					t.Fatalf("continue from %s: reply %q, want a tool error", c.who, lines)
				}
				refusals = append(refusals, lines[0])
				time.Sleep(4 * _pace)
				if got := getThread(t, s, helper).RunID; got != first.ID {
					t.Errorf("continue from %s (%q) started helper run %s, want it refused", c.who, lines[0], got)
				}
			}

			quietBoss(t, s, boss.ID, tester.ID, 1)
			lines := useAtoll(t, s, boss, ev, continueCall(helper, _askPrompt))
			if len(lines) == 0 || lines[0] == "atoll/continue: unavailable" || slices.Contains(refusals, lines[0]) {
				t.Fatalf("continue from the boss thread: reply %q, want it accepted (refusals were %q)", lines, refusals)
			}
			deadline := time.Now().Add(30 * time.Second)
			for getThread(t, s, helper).RunID == first.ID {
				if time.Now().After(deadline) {
					t.Fatalf("continue from the boss thread (%q) gave the helper no new run in 30s", lines[0])
				}
				time.Sleep(_pace)
			}
		})
	}
}
