package e2e

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// _noUsagePrompt plays no-tool-call in testdata/codex: a run that reports no usage.
const _noUsagePrompt = "Explain the build in two paragraphs."

// usageReport is GET /api/usage. A cost is raw JSON so a test tells null from 0 and from absent.
type usageReport struct {
	Total struct {
		CostUSD json.RawMessage `json:"cost_usd"`
		Tokens  int             `json:"tokens"`
		Runs    int             `json:"runs"`
	} `json:"total"`
	Days []struct {
		Day     string          `json:"day"`
		CostUSD json.RawMessage `json:"cost_usd"`
		Tokens  int             `json:"tokens"`
	} `json:"days"`
	Bots []struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Runs    int             `json:"runs"`
		Tokens  int             `json:"tokens"`
		CostUSD json.RawMessage `json:"cost_usd"`
	} `json:"bots"`
	Models []struct {
		Model   string          `json:"model"`
		Tokens  int             `json:"tokens"`
		CostUSD json.RawMessage `json:"cost_usd"`
	} `json:"models"`
}

// usageRow is what a test wants of a total, a day, a Bot or a model; a nil cost wants JSON null.
type usageRow struct {
	runs, tokens int
	cost         *float64
}

// rawCost is a cost as the test reads it: null is nil, a number is itself, anything else fails.
func rawCost(t *testing.T, what string, raw json.RawMessage) *float64 {
	t.Helper()
	if string(raw) == "null" {
		return nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Errorf("%s cost_usd is %q, want a number or null", what, raw)
		return nil
	}
	return &v
}

func wantRowCost(t *testing.T, what string, raw json.RawMessage, want *float64) {
	t.Helper()
	got := rawCost(t, what, raw)
	if got == nil && want == nil {
		return
	}
	if got == nil || want == nil || math.Abs(*got-*want) > 1e-6 {
		t.Errorf("%s cost_usd = %s, want %s", what, raw, showCostJSON(want))
	}
}

func showCostJSON(c *float64) string {
	if c == nil {
		return "null"
	}
	return fmt.Sprintf("%g", *c)
}

// usage reads GET /api/usage for a range, with the zone offset in minutes east of UTC the way a
// page sends it: -new Date().getTimezoneOffset().
func (s *server) usage(t *testing.T, rng string, tzOffset int) usageReport {
	t.Helper()
	var u usageReport
	s.do(t, "GET", fmt.Sprintf("/api/usage?range=%s&tz_offset=%d", rng, tzOffset), nil, &u, http.StatusOK)
	return u
}

// localDay is the day of at for a viewer tzOffset minutes east of UTC.
func localDay(at time.Time, tzOffset int) string {
	return at.UTC().Add(time.Duration(tzOffset) * time.Minute).Format("2006-01-02")
}

// TestUsage_SumsEachRunUnderItsOwnBot: a Claude Bot, Lead, works in a thread of its own and as a
// boss that delegates to a Codex Bot, Tester, which also works in a thread of its own. Runs are
// then moved back in time so each range holds a different set. Every number is the recordings':
// tokens are input + output + cache read + cache write, cost is the run's (Claude only).
//
//	A  Lead, own thread    run-cost-1   claude-sonnet-5-5  21+1037+71620+13150 = 85828  $0.19      3 days ago
//	B  Lead, own thread    run-cost-2   claude-sonnet-5-5  8+365+40000+700     = 41073  $0.05      100 days ago
//	C  Lead, boss thread   mcp-use      claude-haiku-5-5   no token counts     = 0      $0.009056  20 days ago
//	D  Lead, boss thread   wake         claude-haiku-5-5   2+6+40359+141       = 40508  $0.001312  now
//	E  Tester, helper      run-cost-1   gpt-5.6-sol        1200+125+26100+500  = 27925  null       3 days ago
//	F  Tester, own thread  no-tool-call no usage                                                   3 days ago
//
// C's cost is its session's $0.0090556 to the millionth; D's is the session's $0.01036805 less C's.
func TestUsage_SumsEachRunUnderItsOwnBot(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	m.signIn(t, "codex")
	s := m.serve(t)
	lead, tester := s.newBot(t, "Lead", "claude"), s.newBot(t, "Tester", "codex")
	setDelegates(t, s, lead, tester)

	own := s.newThread(t, lead)
	ownEv := s.events(t, own)
	a := s.finished(t, ownEv, s.send(t, own, _costFirst).ID)
	b := s.finished(t, ownEv, s.send(t, own, _costSecond).ID)

	boss := s.newThread(t, lead)
	payload, _ := json.Marshal([]fake.MCPCall{delegateCall("Tester", _costFirst, "")})
	c := s.finished(t, s.events(t, boss), s.send(t, boss, _mcpPromptHead+string(payload)).ID)
	e := settled(t, s, delegated(t, c.Reply))
	quietBoss(t, s, boss.ID, tester.ID, 1)
	d := s.run(t, getThread(t, s, boss.ID).RunID)

	plain := s.newThread(t, tester)
	f := s.finished(t, s.events(t, plain), s.send(t, plain, _noUsagePrompt).ID)

	for name, r := range map[string]runInfo{"A": a, "B": b, "C": c, "D": d, "E": e, "F": f} {
		if r.Status != "done" {
			t.Fatalf("run %s (%s): status %q, error %q; want done", name, r.ID, r.Status, r.Error)
		}
	}
	if d.ID == c.ID {
		t.Fatalf("boss thread %s has no run after the helper's end, want the wake run", boss.ID)
	}
	var fRun struct {
		Usage json.RawMessage `json:"usage"`
	}
	s.do(t, "GET", "/api/runs/"+f.ID, nil, &fRun, http.StatusOK)
	if len(fRun.Usage) != 0 && string(fRun.Usage) != "null" {
		t.Fatalf("run F %s has usage %s, want none", f.ID, fRun.Usage)
	}

	const day = int64(24 * time.Hour / time.Microsecond)
	ago := map[string]int{"A": 3, "B": 100, "C": 20, "D": 0, "E": 3, "F": 3}
	ids := map[string]string{"A": a.ID, "B": b.ID, "C": c.ID, "D": d.ID, "E": e.ID, "F": f.ID}
	for name, n := range ago {
		if n == 0 {
			continue
		}
		if got := dbExec(t, m, fmt.Sprintf("UPDATE runs SET started_at = started_at - %d, finished_at = finished_at - %d WHERE id = '%s'",
			int64(n)*day, int64(n)*day, ids[name])); got != 1 {
			t.Fatalf("moved %d rows for run %s, want 1", got, name)
		}
	}
	finished := map[string]time.Time{}
	for name, id := range ids {
		var r struct {
			FinishedAt time.Time `json:"finished_at"`
		}
		s.do(t, "GET", "/api/runs/"+id, nil, &r, http.StatusOK)
		finished[name] = r.FinishedAt
	}

	usd := func(v float64) *float64 { return &v }
	const (
		tA, tB, tC, tD, tE = 85828, 41073, 0, 40508, 27925
		cA, cB, cC, cD     = 0.19, 0.05, 0.009056, 0.001312
	)
	tokens := map[string]int{"A": tA, "B": tB, "C": tC, "D": tD, "E": tE}
	costs := map[string]float64{"A": cA, "B": cB, "C": cC, "D": cD}
	type usageWant struct {
		runs   []string // the runs with usage in the range
		total  usageRow
		bots   map[string]usageRow // by Bot id
		models map[string]usageRow
	}
	for _, tc := range []struct {
		rng  string
		want usageWant
	}{
		{"24h", usageWant{
			runs:   []string{"D"},
			total:  usageRow{1, tD, usd(cD)},
			bots:   map[string]usageRow{lead.ID: {1, tD, usd(cD)}},
			models: map[string]usageRow{"claude-haiku-5-5": {0, tD, usd(cD)}},
		}},
		{"7d", usageWant{
			runs:  []string{"A", "D", "E"},
			total: usageRow{3, tA + tD + tE, usd(cA + cD)},
			bots:  map[string]usageRow{lead.ID: {2, tA + tD, usd(cA + cD)}, tester.ID: {1, tE, nil}},
			models: map[string]usageRow{
				"claude-haiku-5-5": {0, tD, usd(cD)}, "claude-sonnet-5-5": {0, tA, usd(cA)}, "gpt-5.6-sol": {0, tE, nil},
			},
		}},
		{"30d", usageWant{
			runs:  []string{"A", "C", "D", "E"},
			total: usageRow{4, tA + tC + tD + tE, usd(cA + cC + cD)},
			bots:  map[string]usageRow{lead.ID: {3, tA + tC + tD, usd(cA + cC + cD)}, tester.ID: {1, tE, nil}},
			models: map[string]usageRow{
				"claude-haiku-5-5": {0, tC + tD, usd(cC + cD)}, "claude-sonnet-5-5": {0, tA, usd(cA)}, "gpt-5.6-sol": {0, tE, nil},
			},
		}},
		{"all", usageWant{
			runs:  []string{"A", "B", "C", "D", "E"},
			total: usageRow{5, tA + tB + tC + tD + tE, usd(cA + cB + cC + cD)},
			bots:  map[string]usageRow{lead.ID: {4, tA + tB + tC + tD, usd(cA + cB + cC + cD)}, tester.ID: {1, tE, nil}},
			models: map[string]usageRow{
				"claude-haiku-5-5": {0, tC + tD, usd(cC + cD)}, "claude-sonnet-5-5": {0, tA + tB, usd(cA + cB)}, "gpt-5.6-sol": {0, tE, nil},
			},
		}},
	} {
		// UTC+14 and UTC-12 are 26 hours apart, so a run never falls on the same day in both.
		for _, tz := range []int{840, -720} {
			t.Run(fmt.Sprintf("%s tz_offset=%d", tc.rng, tz), func(t *testing.T) {
				u := s.usage(t, tc.rng, tz)
				w := tc.want

				if u.Total.Runs != w.total.runs || u.Total.Tokens != w.total.tokens {
					t.Errorf("total runs %d, tokens %d; want %d, %d", u.Total.Runs, u.Total.Tokens, w.total.runs, w.total.tokens)
				}
				wantRowCost(t, "total", u.Total.CostUSD, w.total.cost)

				var botRuns int
				var botCost float64
				seen := map[string]bool{}
				for _, got := range u.Bots {
					label := "Bot " + got.ID
					if seen[got.ID] {
						t.Errorf("%s is listed twice", label)
					}
					seen[got.ID] = true
					want, ok := w.bots[got.ID]
					if !ok {
						t.Errorf("%s (%s) is listed with %d runs, want only Bots with runs in the range", label, got.Name, got.Runs)
						continue
					}
					wantName := map[string]string{lead.ID: "Lead", tester.ID: "Tester"}[got.ID]
					if got.Name != wantName || got.Runs != want.runs || got.Tokens != want.tokens {
						t.Errorf("%s: name %q, runs %d, tokens %d; want %q, %d, %d", label, got.Name, got.Runs, got.Tokens, wantName, want.runs, want.tokens)
					}
					wantRowCost(t, label, got.CostUSD, want.cost)
					botRuns += got.Runs
					if c := rawCost(t, label, got.CostUSD); c != nil {
						botCost += *c
					}
				}
				for id := range w.bots {
					if !seen[id] {
						t.Errorf("Bot %s is not listed, want it", id)
					}
				}
				if total := rawCost(t, "total", u.Total.CostUSD); botRuns != u.Total.Runs || total == nil || math.Abs(botCost-*total) > 1e-6 {
					t.Errorf("Bots add up to %d runs, $%g; total is %d runs, %s", botRuns, botCost, u.Total.Runs, u.Total.CostUSD)
				}

				var models []string
				for _, got := range u.Models {
					models = append(models, got.Model)
					want, ok := w.models[got.Model]
					if !ok {
						continue
					}
					if got.Tokens != want.tokens {
						t.Errorf("model %s: tokens %d, want %d", got.Model, got.Tokens, want.tokens)
					}
					wantRowCost(t, "model "+got.Model, got.CostUSD, want.cost)
				}
				var wantModels []string
				for model := range w.models {
					wantModels = append(wantModels, model)
				}
				slices.Sort(models)
				slices.Sort(wantModels)
				if !slices.Equal(models, wantModels) {
					t.Errorf("models %q, want %q, each once", models, wantModels)
				}

				wantDays := map[string]*usageRow{}
				for _, name := range w.runs {
					at := localDay(finished[name], tz)
					if wantDays[at] == nil {
						wantDays[at] = &usageRow{}
					}
					row := wantDays[at]
					row.tokens += tokens[name]
					if c, ok := costs[name]; ok {
						prev := 0.0
						if row.cost != nil {
							prev = *row.cost
						}
						row.cost = usd(prev + c)
					}
				}
				var days, wantDayList []string
				for _, got := range u.Days {
					days = append(days, got.Day)
					want := wantDays[got.Day]
					if want == nil {
						continue
					}
					if got.Tokens != want.tokens {
						t.Errorf("day %s: tokens %d, want %d", got.Day, got.Tokens, want.tokens)
					}
					wantRowCost(t, "day "+got.Day, got.CostUSD, want.cost)
				}
				for k := range wantDays {
					wantDayList = append(wantDayList, k)
				}
				slices.Sort(days)
				slices.Sort(wantDayList)
				if !slices.Equal(days, wantDayList) {
					t.Errorf("days %q, want %q: the viewer's local days with runs, each once, none without", days, wantDayList)
				}
			})
		}
	}
}

// TestUsage_UnknownRangeIsRefusedInPlainWords: a range other than 24h, 7d, 30d and all answers 400
// with Reef's designed error: code invalid and a plain sentence, no raw error or JSON.
func TestUsage_UnknownRangeIsRefusedInPlainWords(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	for _, rng := range []string{"1y", "week", "7days"} {
		got := s.fails(t, "GET", "/api/usage?range="+rng+"&tz_offset=0", nil, http.StatusBadRequest)
		if got.Code != "invalid" || got.Error == "" || strings.ContainsAny(got.Error, "{}") {
			t.Errorf("range=%s: reply %+v, want code invalid and a plain sentence", rng, got)
		}
	}
}
