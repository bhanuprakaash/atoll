package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Claude sign-in's tokens. Neither may reach a response or the server log.
const (
	_usageToken   = "sk-ant-oat01-reef-e2e-usage-token"
	_refreshToken = "sk-ant-ort01-reef-e2e-refresh-token"
)

// planLimits is one entry of GET /api/usage/limits.
type planLimits struct {
	Provider string       `json:"provider"`
	Session  *limitWindow `json:"session"`
	Weekly   *limitWindow `json:"weekly"`
	Error    string       `json:"error"`
}

type limitWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	ResetsAt      string  `json:"resets_at"`
	WindowMinutes int     `json:"window_minutes"`
}

// claudeUsage stands in for GET https://api.anthropic.com/api/oauth/usage as the spike saw it:
// the set answer to a GET with the accepted bearer token and the oauth beta header, 401
// authentication_error to anything else.
type claudeUsage struct {
	mu     sync.Mutex
	token  string
	status int
	body   string
	reads  int
}

func (c *claudeUsage) answer(status int, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status, c.body = status, body
}

// accept makes the endpoint take only token, as after the user signed in elsewhere.
func (c *claudeUsage) accept(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *claudeUsage) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// claudeUsage points the machine's ATOLL_CLAUDE_USAGE_URL at a fake usage endpoint that takes
// _usageToken.
func (m *machine) claudeUsage(t *testing.T) *claudeUsage {
	t.Helper()
	c := &claudeUsage{token: _usageToken}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/api/oauth/usage" ||
			r.Header.Get("Authorization") != "Bearer "+c.token || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"Invalid bearer token"}}`)
			return
		}
		c.reads++
		w.WriteHeader(c.status)
		_, _ = fmt.Fprint(w, c.body)
	}))
	t.Cleanup(srv.Close)
	m.env = append(m.env, "ATOLL_CLAUDE_USAGE_URL="+srv.URL+"/api/oauth/usage")
	return c
}

// signInClaude leaves a Claude sign-in holding _usageToken and _refreshToken, valid for hours,
// where the real CLI keeps it on Linux.
func (m *machine) signInClaude(t *testing.T) {
	t.Helper()
	writeFile(t, filepath.Join(m.providerDir("claude"), ".credentials.json"), fmt.Sprintf(
		`{"claudeAiOauth":{"accessToken":%q,"refreshToken":%q,"expiresAt":%d,"scopes":["user:inference","user:profile"],"subscriptionType":"max"}}`,
		_usageToken, _refreshToken, time.Now().Add(8*time.Hour).UnixMilli()))
}

// claudeAnswer is the usage endpoint's body with five_hour and seven_day at the given
// utilization and reset times, among keys Reef ignores.
func claudeAnswer(session, weekly float64, sessionAt, weeklyAt time.Time) string {
	at := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05") + ".699683+00:00" }
	return fmt.Sprintf(`{"five_hour":{"utilization":%.1f,"resets_at":%q},"seven_day":{"utilization":%.1f,"resets_at":%q},`+
		`"seven_day_oauth_apps":null,"seven_day_opus":{"utilization":0.0,"resets_at":null},"extra_usage":{"is_enabled":false,"monthly_limit":null}}`,
		session, at(sessionAt), weekly, at(weeklyAt))
}

// codexWindow is one window of the app-server's rate-limit snapshot.
type codexWindow struct {
	used, minutes int
	at            time.Time
}

// codexLimits makes the fake codex answer account/rateLimits/read with primary and secondary; a
// nil secondary is null, as on the free plan.
func (m *machine) codexLimits(t *testing.T, primary codexWindow, secondary *codexWindow) {
	t.Helper()
	win := func(w codexWindow) map[string]any {
		return map[string]any{"usedPercent": w.used, "windowDurationMins": w.minutes, "resetsAt": w.at.Unix()}
	}
	limits := map[string]any{
		"limitId": "codex", "limitName": nil, "primary": win(primary), "secondary": nil,
		"credits": nil, "planType": "plus", "rateLimitReachedType": nil,
	}
	if secondary != nil {
		limits["secondary"] = win(*secondary)
	}
	b, err := json.Marshal(map[string]any{"rateLimits": limits})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(m.bin, "rate-limits.json"), string(b))
}

// limits reads GET /api/usage/limits. It fails the test if the answer is not a JSON array, if an
// entry is neither {provider, session, weekly} nor {provider, error}, or if a token of the Claude
// sign-in shows in the answer or the server log.
func (s *server) limits(t *testing.T) []planLimits {
	t.Helper()
	var raw json.RawMessage
	s.do(t, "GET", "/api/usage/limits", nil, &raw, http.StatusOK)
	for _, secret := range []string{_usageToken, _refreshToken} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("GET /api/usage/limits holds a token of the Claude sign-in: %s", raw)
		}
		if strings.Contains(s.log.String(), secret) {
			t.Errorf("the server log holds a token of the Claude sign-in:\n%s", s.log)
		}
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		t.Fatalf("GET /api/usage/limits = %s, want a JSON array", raw)
	}
	var got []planLimits
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("GET /api/usage/limits = %s: %v", raw, err)
	}
	for i, e := range entries {
		_, hasSession := e["session"]
		_, hasWeekly := e["weekly"]
		_, hasError := e["error"]
		failed := got[i].Error != ""
		if hasError && !failed || !failed && (!hasSession || !hasWeekly) ||
			failed && (got[i].Session != nil || got[i].Weekly != nil) {
			t.Errorf("entry %d of %s, want {provider, session, weekly} or {provider, error}", i, raw)
		}
	}
	return got
}

// wantLimits checks the entries' providers in order, and each answered one's windows; a wanted
// entry with an error wants any error.
func wantLimits(t *testing.T, got, want []planLimits) {
	t.Helper()
	providers := func(ls []planLimits) []string {
		p := []string{}
		for _, l := range ls {
			p = append(p, l.Provider)
		}
		return p
	}
	if fmt.Sprint(providers(got)) != fmt.Sprint(providers(want)) {
		t.Fatalf("GET /api/usage/limits providers %q, want %q", providers(got), providers(want))
	}
	for i, w := range want {
		g := got[i]
		switch {
		case w.Error != "" && g.Error == "":
			t.Errorf("%s: %+v, want {provider, error}", g.Provider, g)
		case w.Error == "" && g.Error != "":
			t.Errorf("%s: error %q, want its limits", g.Provider, g.Error)
		case w.Error == "":
			wantWindow(t, g.Provider+" session", g.Session, w.Session)
			wantWindow(t, g.Provider+" weekly", g.Weekly, w.Weekly)
		}
	}
}

func wantWindow(t *testing.T, what string, got, want *limitWindow) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Errorf("%s = %+v, want %+v", what, got, want)
		}
		return
	}
	at, err := time.Parse(time.RFC3339, got.ResetsAt)
	wantAt, _ := time.Parse(time.RFC3339, want.ResetsAt)
	if err != nil || !at.Truncate(time.Second).Equal(wantAt) {
		t.Errorf("%s resets_at %q (%v), want RFC 3339 %s", what, got.ResetsAt, err, want.ResetsAt)
	}
	if got.UsedPercent != want.UsedPercent || got.WindowMinutes != want.WindowMinutes {
		t.Errorf("%s: used_percent %g, window_minutes %d; want %g, %d",
			what, got.UsedPercent, got.WindowMinutes, want.UsedPercent, want.WindowMinutes)
	}
}

func window(used float64, at time.Time, minutes int) *limitWindow {
	return &limitWindow{UsedPercent: used, ResetsAt: at.UTC().Format(time.RFC3339), WindowMinutes: minutes}
}

// TestUsageLimits_EachSignedInProviderSessionAndWeekly: with Claude and Codex signed in, GET
// /api/usage/limits answers Claude then Codex. Claude's five_hour is the session (300 minutes) and
// seven_day the weekly (10080); a Codex window of a day or less is the session, a longer one the
// weekly. Nothing is cached: a second call shows each provider's new answer, a Codex free plan's
// lone 30-day window among them.
func TestUsageLimits_EachSignedInProviderSessionAndWeekly(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude", "codex")
	m.signInClaude(t)
	m.signIn(t, "codex")
	claude := m.claudeUsage(t)
	now := time.Now().Truncate(time.Second)
	cSession, cWeekly := now.Add(3*time.Hour), now.Add(4*24*time.Hour)
	xSession, xWeekly := now.Add(2*time.Hour), now.Add(6*24*time.Hour)
	claude.answer(http.StatusOK, claudeAnswer(52, 18, cSession, cWeekly))
	m.codexLimits(t, codexWindow{92, 300, xSession}, &codexWindow{40, 10080, xWeekly})
	s := m.serve(t)

	wantLimits(t, s.limits(t), []planLimits{
		{Provider: "claude", Session: window(52, cSession, 300), Weekly: window(18, cWeekly, 10080)},
		{Provider: "codex", Session: window(92, xSession, 300), Weekly: window(40, xWeekly, 10080)},
	})

	xMonth := now.Add(20 * 24 * time.Hour)
	claude.answer(http.StatusOK, claudeAnswer(61, 25, cSession, cWeekly))
	m.codexLimits(t, codexWindow{7, 43200, xMonth}, nil)
	wantLimits(t, s.limits(t), []planLimits{
		{Provider: "claude", Session: window(61, cSession, 300), Weekly: window(25, cWeekly, 10080)},
		{Provider: "codex", Session: nil, Weekly: window(7, xMonth, 43200)},
	})
	if n := claude.count(); n != 2 {
		t.Errorf("Claude's usage endpoint was read %d times for two calls, want 2: one read per call", n)
	}
}

// TestUsageLimits_SignedOutProviderIsAbsent: a provider that is installed but not signed in has
// no entry; with none signed in the answer is an empty array.
func TestUsageLimits_SignedOutProviderIsAbsent(t *testing.T) {
	t.Parallel()
	at := time.Now().Truncate(time.Second).Add(time.Hour)
	for _, tc := range []struct {
		name     string
		signedIn []string
		want     []planLimits
	}{
		{"codex only", []string{"codex"}, []planLimits{{Provider: "codex", Session: window(30, at, 300), Weekly: window(30, at, 10080)}}},
		{"claude only", []string{"claude"}, []planLimits{{Provider: "claude", Session: window(30, at, 300), Weekly: window(30, at, 10080)}}},
		{"none", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, "claude", "codex")
			m.claudeUsage(t).answer(http.StatusOK, claudeAnswer(30, 30, at, at))
			m.codexLimits(t, codexWindow{30, 300, at}, &codexWindow{30, 10080, at})
			for _, p := range tc.signedIn {
				if p == "claude" {
					m.signInClaude(t)
				} else {
					m.signIn(t, p)
				}
			}
			s := m.serve(t)
			wantLimits(t, s.limits(t), tc.want)
		})
	}
}

// TestUsageLimits_FailedReadIsPlainWordsAndTheOtherStillAnswers: when one provider's read fails
// (its server errs, refuses the token, or answers a shape Reef does not know) its entry is
// {provider, error} in plain words that do not echo the raw answer, and the other provider still
// answers. No token of the sign-in shows in the answer or the server log.
func TestUsageLimits_FailedReadIsPlainWordsAndTheOtherStillAnswers(t *testing.T) {
	t.Parallel()
	at := time.Now().Truncate(time.Second).Add(time.Hour)
	claudeOK := planLimits{Provider: "claude", Session: window(30, at, 300), Weekly: window(30, at, 10080)}
	codexOK := planLimits{Provider: "codex", Session: window(30, at, 300), Weekly: window(30, at, 10080)}
	failed := func(p string) planLimits { return planLimits{Provider: p, Error: "any"} }
	for _, tc := range []struct {
		name  string
		setUp func(*testing.T, *machine, *claudeUsage)
		raw   string // what of Claude's raw answer must not show in the error
		want  []planLimits
	}{
		{"claude server error", func(_ *testing.T, _ *machine, c *claudeUsage) {
			c.answer(http.StatusInternalServerError, `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`)
		}, "api_error", []planLimits{failed("claude"), codexOK}},
		{"claude refuses the token", func(_ *testing.T, _ *machine, c *claudeUsage) {
			c.accept("sk-ant-oat01-another-sign-in")
		}, "authentication_error", []planLimits{failed("claude"), codexOK}},
		{"claude unexpected shape", func(_ *testing.T, _ *machine, c *claudeUsage) {
			c.answer(http.StatusOK, `{"five_hour":"52%","seven_day":"18%"}`)
		}, "52%", []planLimits{failed("claude"), codexOK}},
		{"codex read fails", func(t *testing.T, m *machine, _ *claudeUsage) {
			if err := os.Remove(filepath.Join(m.bin, "rate-limits.json")); err != nil {
				t.Fatal(err)
			}
		}, "", []planLimits{claudeOK, failed("codex")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, "claude", "codex")
			m.signInClaude(t)
			m.signIn(t, "codex")
			c := m.claudeUsage(t)
			c.answer(http.StatusOK, claudeAnswer(30, 30, at, at))
			m.codexLimits(t, codexWindow{30, 300, at}, &codexWindow{30, 10080, at})
			tc.setUp(t, m, c)
			s := m.serve(t)

			got := s.limits(t)
			wantLimits(t, got, tc.want)
			for _, g := range got {
				if g.Error != "" && (strings.ContainsAny(g.Error, "{}") || tc.raw != "" && strings.Contains(g.Error, tc.raw)) {
					t.Errorf("%s: error %q, want a plain sentence without the raw answer (%q)", g.Provider, g.Error, tc.raw)
				}
			}
		})
	}
}
