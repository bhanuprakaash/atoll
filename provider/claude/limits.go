package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"golang.org/x/text/unicode/norm"

	"atoll/provider"
)

const (
	_usageURL     = "https://api.anthropic.com/api/oauth/usage"
	_usageBeta    = "oauth-2025-04-20"
	_usageTimeout = 10 * time.Second
	_usageMax     = 1 << 20
	_sessionMins  = 300
	_weeklyMins   = 7 * 24 * 60
)

const (
	_errSignIn  = limitsError("Reef could not read Claude's sign-in.")
	_errReach   = limitsError("Claude's usage service could not be reached.")
	_errAnswer  = limitsError("Claude's usage service gave an answer Reef does not know.")
	_errRefused = limitsError("Claude's usage service refused the sign-in.")
)

var _account = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// limitsError is an error in plain words for the person: it holds no text of the provider's.
type limitsError string

func (e limitsError) Error() string { return string(e) }

type credentials struct {
	OAuth struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

type usageWindow struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}

// Limits reads the plan limits of the Claude sign-in in dir from Claude's usage endpoint,
// ATOLL_CLAUDE_USAGE_URL or Anthropic's. That endpoint is not a documented API. An expired token
// is refreshed by the CLI itself (`claude auth status`), never by Reef. The token goes nowhere
// but that request.
func Limits(ctx context.Context, bin, dir string) (provider.Limits, error) {
	token, err := accessToken(ctx, bin, dir)
	if err != nil {
		return provider.Limits{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, _usageTimeout)
	defer cancel()
	url := _usageURL
	if u := os.Getenv("ATOLL_CLAUDE_USAGE_URL"); u != "" {
		url = u
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return provider.Limits{}, _errReach
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", _usageBeta)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return provider.Limits{}, _errReach
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(res.Body, _usageMax))
	switch {
	case err != nil:
		return provider.Limits{}, _errReach
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return provider.Limits{}, _errRefused
	case res.StatusCode != http.StatusOK:
		return provider.Limits{}, _errReach
	}
	var body struct {
		FiveHour *usageWindow `json:"five_hour"`
		SevenDay *usageWindow `json:"seven_day"`
	}
	if json.Unmarshal(b, &body) != nil {
		return provider.Limits{}, _errAnswer
	}
	return provider.Limits{
		Session: body.FiveHour.window(_sessionMins),
		Weekly:  body.SevenDay.window(_weeklyMins),
	}, nil
}

// window is nil for a window the answer lacks or that has no reset time.
func (w *usageWindow) window(minutes int) *provider.Window {
	if w == nil || w.ResetsAt == nil {
		return nil
	}
	return &provider.Window{UsedPercent: w.Utilization, ResetsAt: *w.ResetsAt, WindowMinutes: minutes}
}

// accessToken is the sign-in's access token. When it has expired, the CLI refreshes it.
func accessToken(ctx context.Context, bin, dir string) (string, error) {
	c, err := readCredentials(ctx, dir)
	if err == nil && c.OAuth.ExpiresAt > 0 && c.OAuth.ExpiresAt <= time.Now().UnixMilli() {
		_, _ = SignedIn(ctx, bin, dir)
		c, err = readCredentials(ctx, dir)
	}
	if err != nil {
		return "", err
	}
	return c.OAuth.AccessToken, nil
}

// readCredentials reads where the CLI keeps its sign-in: the macOS Keychain, else
// .credentials.json in dir.
func readCredentials(ctx context.Context, dir string) (credentials, error) {
	var b []byte
	var err error
	if runtime.GOOS == "darwin" {
		b, err = exec.CommandContext(ctx, "security", keychainArgs(dir)...).Output()
	} else {
		b, err = os.ReadFile(filepath.Join(dir, ".credentials.json"))
	}
	var c credentials
	if err != nil || json.Unmarshal(b, &c) != nil || c.OAuth.AccessToken == "" {
		return credentials{}, _errSignIn
	}
	return c, nil
}

// keychainArgs are the arguments of `security` that print the Keychain item the CLI makes for the
// config folder dir.
func keychainArgs(dir string) []string {
	account := os.Getenv("USER")
	if !_account.MatchString(account) {
		account = "claude-code-user"
	}
	sum := sha256.Sum256([]byte(norm.NFC.String(dir)))
	service := "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
	return []string{"find-generic-password", "-a", account, "-w", "-s", service}
}
