package e2e

import (
	"fmt"
	"math"
	"net/http"
	"testing"
)

// Prompts of the recordings run-cost-1 and run-cost-2 in testdata/claude and testdata/codex: the
// second resumes the session of the first.
const (
	_costFirst  = "Cost check: look around the folder six times, then answer."
	_costSecond = "Cost check, second round: look again and answer."
)

// wantUsage is what a run's usage says. A nil cost means the field is absent or null.
type wantUsage struct {
	model                                string
	calls                                int
	input, output, cacheRead, cacheWrite int
	cost                                 *float64
}

func cost(v float64) *float64 { return &v }

func TestRunCost_RecordsUsagePerRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		provider      string
		first, second wantUsage
	}{
		{
			// result.usage is the process's; total_cost_usd and modelUsage are the session's, so the
			// second run's cost is 0.24 - 0.19 and its tokens are not the session's 111k.
			provider: "claude",
			first:    wantUsage{"claude-sonnet-5-5", 6, 21, 1037, 71620, 13150, cost(0.19)},
			second:   wantUsage{"claude-sonnet-5-5", 2, 8, 365, 40000, 700, cost(0.05)},
		},
		{
			// A resume first replays an update of the earlier turn and tokenUsage.total is the
			// thread's: a run sums the `last` of its own turn. Input is inputTokens less
			// cachedInputTokens. No cost.
			provider: "codex",
			first:    wantUsage{"gpt-5.6-sol", 3, 1200, 125, 26100, 500, nil},
			second:   wantUsage{"gpt-5.6-sol", 2, 200, 75, 18500, 200, nil},
		},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			_, s := start(t, tc.provider)
			b := s.newBot(t, "Ada", tc.provider)
			th := s.newThread(t, b)
			ev := s.events(t, th)

			first := s.finished(t, ev, s.send(t, th, _costFirst).ID)
			s.assertUsage(t, first.ID, tc.first)
			second := s.finished(t, ev, s.send(t, th, _costSecond).ID)
			s.assertUsage(t, second.ID, tc.second)
			s.assertUsage(t, first.ID, tc.first) // the later run leaves it alone

			// The cost stays in the run's usage, asserted above; the Bots list no longer carries it.
			var bots []map[string]any
			s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
			found := false
			for _, got := range bots {
				if _, ok := got["cost_24h_usd"]; ok {
					t.Errorf("GET /api/bots: Bot %v has cost_24h_usd, want no such key", got["id"])
				}
				found = found || got["id"] == b.ID
			}
			if !found {
				t.Fatalf("GET /api/bots has no Bot %s", b.ID)
			}
		})
	}
}

// assertUsage reads the run and compares its usage to want, key by key.
func (s *server) assertUsage(t *testing.T, runID string, want wantUsage) {
	t.Helper()
	var r struct {
		Usage map[string]any `json:"usage"`
	}
	s.do(t, "GET", "/api/runs/"+runID, nil, &r, http.StatusOK)
	if r.Usage == nil {
		t.Fatalf("GET /api/runs/%s has no usage after run_finished", runID)
	}
	for k, v := range map[string]int{
		"calls": want.calls, "input_tokens": want.input, "output_tokens": want.output,
		"cache_read_tokens": want.cacheRead, "cache_write_tokens": want.cacheWrite,
	} {
		if got, ok := r.Usage[k].(float64); !ok || int(got) != v {
			t.Errorf("run %s usage.%s = %v, want %d", runID, k, r.Usage[k], v)
		}
	}
	if r.Usage["model"] != want.model {
		t.Errorf("run %s usage.model = %v, want %q", runID, r.Usage["model"], want.model)
	}
	var got *float64
	if v, ok := r.Usage["cost_usd"].(float64); ok {
		got = &v
	} else if r.Usage["cost_usd"] != nil {
		t.Errorf("run %s usage.cost_usd = %v, want a number or nothing", runID, r.Usage["cost_usd"])
	}
	if !sameCost(got, want.cost) {
		t.Errorf("run %s usage.cost_usd = %s, want %s", runID, showCost(got), showCost(want.cost))
	}
}

func sameCost(got, want *float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil || got != nil && *got == 0
	}
	return math.Abs(*got-*want) < 1e-6
}

func showCost(c *float64) string {
	if c == nil {
		return "none"
	}
	return fmt.Sprintf("$%g", *c)
}
