package server

import (
	"cmp"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// usageSum counts runs. A nil cost means no run had a price, never 0.
type usageSum struct {
	runs   int
	tokens int
	cost   *float64
}

func (u *usageSum) add(tokens int, cost *float64) {
	u.runs++
	u.tokens += tokens
	if cost == nil {
		return
	}
	if u.cost == nil {
		u.cost = new(float64)
	}
	*u.cost += *cost
}

type (
	usageTotal struct {
		CostUSD *float64 `json:"cost_usd"`
		Tokens  int      `json:"tokens"`
		Runs    int      `json:"runs"`
	}
	usageDay struct {
		Day     string   `json:"day"`
		CostUSD *float64 `json:"cost_usd"`
		Tokens  int      `json:"tokens"`
	}
	usageBot struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Runs    int      `json:"runs"`
		Tokens  int      `json:"tokens"`
		CostUSD *float64 `json:"cost_usd"`
	}
	usageModel struct {
		Model   string   `json:"model"`
		Tokens  int      `json:"tokens"`
		CostUSD *float64 `json:"cost_usd"`
	}
)

func (a *api) getUsage(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	switch r.URL.Query().Get("range") {
	case "24h":
		since = time.Now().Add(-24 * time.Hour)
	case "7d":
		since = time.Now().AddDate(0, 0, -7)
	case "30d":
		since = time.Now().AddDate(0, 0, -30)
	case "all":
	default:
		fail(w, http.StatusBadRequest, "The range must be 24h, 7d, 30d or all.")
		return
	}
	var offset int
	if v := r.URL.Query().Get("tz_offset"); v != "" {
		var err error
		if offset, err = strconv.Atoi(v); err != nil {
			fail(w, http.StatusBadRequest, "The time zone offset must be a number of minutes.")
			return
		}
	}
	runs, err := a.st.Usage(r.Context(), since)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	var total usageSum
	days, bots, models := map[string]*usageSum{}, map[string]*usageSum{}, map[string]*usageSum{}
	names := map[string]string{}
	for _, run := range runs {
		u := run.Usage
		tokens := u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
		day := run.FinishedAt.Add(time.Duration(offset) * time.Minute).Format(time.DateOnly)
		names[run.BotID] = run.BotName
		total.add(tokens, u.CostUSD)
		sumOf(days, day).add(tokens, u.CostUSD)
		sumOf(bots, run.BotID).add(tokens, u.CostUSD)
		sumOf(models, u.Model).add(tokens, u.CostUSD)
	}

	out := struct {
		Total  usageTotal   `json:"total"`
		Days   []usageDay   `json:"days"`
		Bots   []usageBot   `json:"bots"`
		Models []usageModel `json:"models"`
	}{
		Total:  usageTotal{CostUSD: total.cost, Tokens: total.tokens, Runs: total.runs},
		Days:   []usageDay{},
		Bots:   []usageBot{},
		Models: []usageModel{},
	}
	for _, day := range slices.Sorted(maps.Keys(days)) {
		out.Days = append(out.Days, usageDay{Day: day, CostUSD: days[day].cost, Tokens: days[day].tokens})
	}
	for id, s := range bots {
		out.Bots = append(out.Bots, usageBot{ID: id, Name: names[id], Runs: s.runs, Tokens: s.tokens, CostUSD: s.cost})
	}
	slices.SortFunc(out.Bots, func(x, y usageBot) int {
		return cmp.Or(byCost(x.CostUSD, y.CostUSD), cmp.Compare(y.Tokens, x.Tokens), cmp.Compare(x.Name, y.Name))
	})
	for model, s := range models {
		out.Models = append(out.Models, usageModel{Model: model, Tokens: s.tokens, CostUSD: s.cost})
	}
	slices.SortFunc(out.Models, func(x, y usageModel) int {
		return cmp.Or(byCost(x.CostUSD, y.CostUSD), cmp.Compare(y.Tokens, x.Tokens), cmp.Compare(x.Model, y.Model))
	})
	write(w, http.StatusOK, out)
}

func sumOf(m map[string]*usageSum, key string) *usageSum {
	if m[key] == nil {
		m[key] = &usageSum{}
	}
	return m[key]
}

// byCost orders the dearer first and a missing cost last.
func byCost(x, y *float64) int {
	switch {
	case x == nil && y == nil:
		return 0
	case x == nil:
		return 1
	case y == nil:
		return -1
	}
	return cmp.Compare(*y, *x)
}
