package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: finish-report-contract.md.

// _finishHead is the prompt of testdata/*/finish-report.jsonl: the run calls only the finish_run
// tool the payload names, with no finish_run of its own after it.
const _finishHead = "Report only. Use MCP tools. "

// finishRun starts a run on a new thread of bot that calls finish_run with args, waits for it to
// end and returns the run and what finish_run answered.
func finishRun(t *testing.T, s *server, b bot, args string) (runInfo, string) {
	t.Helper()
	th := s.newThread(t, b)
	ev := s.events(t, th)
	payload, _ := json.Marshal([]fake.MCPCall{{Server: "atoll", Tool: "finish_run", Args: json.RawMessage(args)}})
	r := s.finished(t, ev, s.send(t, th, _finishHead+string(payload)).ID)
	if r.Status != "done" {
		t.Fatalf("run with finish_run %s: status %q, error %q; want done", args, r.Status, r.Error)
	}
	answer, ok := strings.CutPrefix(r.Reply, "atoll/finish_run: ")
	if !ok {
		t.Fatalf("run with finish_run %s: reply %q, want the answer of finish_run", args, r.Reply)
	}
	return r, answer
}

func TestFinishRun_RefusesNoSummary(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			for _, args := range []string{
				`{}`,
				`{"tried":"none","blocked":"","next":"none"}`,
				`{"done":"","tried":"none","blocked":"","next":"none"}`,
				`{"done":"  \n","tried":"none","blocked":"","next":"none"}`,
			} {
				r, answer := finishRun(t, s, b, args)
				if !strings.Contains(answer, "done") || strings.Contains(answer, "Report saved") {
					t.Errorf("finish_run %s answered %q, want a refusal that names done", args, answer)
				}
				if r.Report != nil {
					t.Errorf("finish_run %s: run %s stored report %+v, want none: the run does not finish on a refused call", args, r.ID, *r.Report)
				}
			}
		})
	}
}

func TestFinishRun_BlockedNothingIsEmpty(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			for _, blocked := range []string{
				"Nothing blocking", "None", "nothing", "n/a",
				"NONE.", "Nothing blocking.", "N/A!", "  none  ", "Nothing Blocking...",
			} {
				args, _ := json.Marshal(report{Done: "Checked the build.", Tried: "none", Blocked: blocked, Next: "none"})
				r, answer := finishRun(t, s, b, string(args))
				if answer != "Report saved." {
					t.Errorf("blocked %q: finish_run answered %q, want %q", blocked, answer, "Report saved.")
				}
				want := report{Done: "Checked the build.", Tried: "none", Next: "none"}
				if r.Report == nil || *r.Report != want {
					t.Errorf("blocked %q: stored report %+v, want %+v with blocked empty, so the thread shows no Blocked line", blocked, r.Report, want)
				}
			}
		})
	}
}

func TestFinishRun_RealBlockedStays(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			for _, blocked := range []string{
				"GitLab sign-in expired; the user must sign in again.",
				"None of the tests ran: the CI token expired.",
			} {
				args, _ := json.Marshal(report{Done: "Opened the MR.", Tried: "none", Blocked: blocked, Next: "none"})
				r, _ := finishRun(t, s, b, string(args))
				want := report{Done: "Opened the MR.", Tried: "none", Blocked: blocked, Next: "none"}
				if r.Report == nil || *r.Report != want {
					t.Errorf("blocked %q: stored report %+v, want it kept as given: %+v", blocked, r.Report, want)
				}
			}
		})
	}
}
