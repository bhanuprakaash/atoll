package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"

	"atoll/store"
)

const _badRefreshToken _oauthError = "bad_refresh_token"

const (
	// GitHubInstance is the instance_url of GitHub connections.
	GitHubInstance = "https://github.com"
	// GitHubClientID is Reef's public GitHub OAuth App: device flow, no secret.
	GitHubClientID = "Ov23li5N8B8tlXHi1Rza"
	_githubScope   = "repo read:org workflow"
	_githubAPI     = "https://api.github.com"
	_githubMCP     = "https://api.githubcopilot.com/mcp/"
)

// ErrGitHubNeedsSignIn is GitHub rejecting the token.
var ErrGitHubNeedsSignIn = errors.New("GitHub needs sign-in")

// GitHubRejection is a 401 from GitHub; it matches ErrGitHubNeedsSignIn.
type GitHubRejection struct {
	Endpoint  string
	Message   string // the message of GitHub's reply
	RequestID string // X-GitHub-Request-Id
	Cause     string // what GitHub rejected
}

// GitHubTokens is the OAuth token record saved for a GitHub connection.
type GitHubTokens struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type githubTokenReply struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_token_expires_in"`
}

// RejectedBy reads the 401 resp from endpoint.
func RejectedBy(endpoint string, resp *http.Response) *GitHubRejection {
	var reply struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply)
	return &GitHubRejection{Endpoint: endpoint, Message: reply.Message,
		RequestID: resp.Header.Get("X-GitHub-Request-Id"), Cause: "access token rejected"}
}

func (e *GitHubRejection) Error() string { return ErrGitHubNeedsSignIn.Error() }

// Is matches ErrGitHubNeedsSignIn.
func (e *GitHubRejection) Is(target error) bool { return target == ErrGitHubNeedsSignIn }

// GitHubURL is the base URL of GitHub's sign-in and the one git's credential helper is scoped
// to: ATOLL_GITHUB_URL, else github.com.
func GitHubURL() string {
	if u := os.Getenv("ATOLL_GITHUB_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return GitHubInstance
}

// GitHubAPI is the base of GitHub's REST API: ATOLL_GITHUB_URL, else api.github.com.
func GitHubAPI() string {
	if os.Getenv("ATOLL_GITHUB_URL") != "" {
		return GitHubURL()
	}
	return _githubAPI
}

// OnGitHub reports whether a git remote URL, in any form git takes, is on the host of GitHubURL.
func OnGitHub(remote string) bool {
	base, err := url.Parse(GitHubURL())
	if err != nil {
		return false
	}
	host := ""
	if u, err := url.Parse(remote); err == nil {
		host = u.Host
	}
	if host == "" {
		_, rest, found := strings.Cut(remote, "@")
		if !found {
			rest = remote
		}
		host, _, _ = strings.Cut(rest, ":")
	}
	return strings.EqualFold(host, base.Host)
}

// GitHubRepo returns the owner and name of the GitHub repo a git remote URL names, in any form
// git takes; ok is false for a remote that is not on the host of GitHubURL.
func GitHubRepo(remote string) (owner, name string, ok bool) {
	if !OnGitHub(remote) {
		return "", "", false
	}
	p := ""
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		p = u.Path
	} else if _, rest, found := strings.Cut(remote, ":"); found {
		p = rest
	}
	owner, name, found := strings.Cut(strings.TrimSuffix(strings.Trim(p, "/"), ".git"), "/")
	return owner, name, found && owner != "" && name != "" && !strings.Contains(name, "/")
}

// GitHubMCPURL is GitHub's MCP server: ATOLL_GITHUB_MCP_URL, else the hosted one.
func GitHubMCPURL() string {
	if u := os.Getenv("ATOLL_GITHUB_MCP_URL"); u != "" {
		return u
	}
	return _githubMCP
}

// StartGitHubDevice starts the device flow.
func StartGitHubDevice(ctx context.Context, c *http.Client) (Device, error) {
	var reply struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	err := githubPost(ctx, c, "/login/device/code", url.Values{
		"client_id": {GitHubClientID}, "scope": {_githubScope},
	}, &reply)
	if err != nil {
		return Device{}, err
	}
	return Device{
		DeviceCode: reply.DeviceCode, UserCode: reply.UserCode,
		VerificationURI: reply.VerificationURI, Interval: time.Duration(reply.Interval) * time.Second,
		ExpiresIn: time.Duration(reply.ExpiresIn) * time.Second,
	}, nil
}

// PollGitHubToken returns ErrPending until the user approves.
func PollGitHubToken(ctx context.Context, c *http.Client, deviceCode string) (GitHubTokens, error) {
	start := time.Now().UTC()
	var reply githubTokenReply
	err := githubPost(ctx, c, "/login/oauth/access_token", url.Values{
		"client_id": {GitHubClientID}, "device_code": {deviceCode},
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
	}, &reply)
	if err != nil {
		return GitHubTokens{}, err
	}
	return reply.tokens(start)
}

// CheckGitHubToken returns ErrGitHubNeedsSignIn when GitHub rejects the token.
func CheckGitHubToken(ctx context.Context, c *http.Client, token string) error {
	endpoint := GitHubAPI() + "/user"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("send GitHub request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return RejectedBy(endpoint, resp)
	}
	return fmt.Errorf("GitHub HTTP status: %d", resp.StatusCode)
}

// SaveGitHubToken stores the tokens of connection id in the OS secret store.
func SaveGitHubToken(id string, tokens GitHubTokens) error {
	lock, _ := _tokenLocks.LoadOrStore(id, &sync.Mutex{})
	mu, _ := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	return saveGitHubTokens(id, tokens)
}

// GitHubToken loads the token of connection id from the OS secret store and refreshes it when
// less than minLife is left. A bare token, or one with no refresh token, is used as it is.
// GitHub rejecting the refresh token returns a *GitHubRejection; a refresh that gets no answer
// leaves the stored tokens alone.
func GitHubToken(ctx context.Context, c *http.Client, log *slog.Logger, id string, minLife time.Duration) (string, error) {
	lock, _ := _tokenLocks.LoadOrStore(id, &sync.Mutex{})
	mu, _ := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	value, err := keyring.Get("atoll", "github:"+id)
	if err != nil {
		return "", store.SecretReadError(err)
	}
	var tokens GitHubTokens
	if json.Unmarshal([]byte(value), &tokens) != nil {
		return value, nil
	}
	if tokens.RefreshToken == "" || time.Until(tokens.ExpiresAt) > minLife {
		return tokens.AccessToken, nil
	}
	start := time.Now().UTC()
	var reply githubTokenReply
	err = githubPost(ctx, c, "/login/oauth/access_token", url.Values{
		"client_id": {GitHubClientID}, "grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken},
	}, &reply)
	if errors.Is(err, _badRefreshToken) {
		return "", &GitHubRejection{Endpoint: GitHubURL() + "/login/oauth/access_token",
			Message: string(_badRefreshToken), Cause: "refresh token rejected"}
	}
	if err != nil {
		return "", err
	}
	refreshed, err := reply.tokens(start)
	if err != nil {
		return "", err
	}
	if err := saveGitHubTokens(id, refreshed); err != nil {
		return "", err
	}
	log.Info("GitHub token refreshed", "connection_id", id, "expires_at", refreshed.ExpiresAt)
	return refreshed.AccessToken, nil
}

func saveGitHubTokens(id string, tokens GitHubTokens) error {
	b, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("encode OAuth tokens: %w", err)
	}
	return keyring.Set("atoll", "github:"+id, string(b))
}

// tokens counts the lifetimes from start, before the request: the token never outlives them.
func (r githubTokenReply) tokens(start time.Time) (GitHubTokens, error) {
	if r.AccessToken == "" {
		return GitHubTokens{}, errors.New("OAuth reply has no access token")
	}
	tokens := GitHubTokens{AccessToken: r.AccessToken, RefreshToken: r.RefreshToken}
	if r.ExpiresIn > 0 {
		tokens.ExpiresAt = start.Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	if r.RefreshExpiresIn > 0 {
		tokens.RefreshExpiresAt = start.Add(time.Duration(r.RefreshExpiresIn) * time.Second)
	}
	return tokens, nil
}

// GitHub answers 200 with an error field, and form-encoded unless asked for JSON.
func githubPost(ctx context.Context, c *http.Client, path string, form url.Values, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GitHubURL()+path, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create OAuth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("send OAuth request: %w: %w", ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OAuth HTTP status: %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decode OAuth response: %w", err)
	}
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &failure) == nil && failure.Error != "" {
		switch failure.Error {
		case "authorization_pending":
			return ErrPending
		case "slow_down":
			return ErrSlowDown
		case string(_badRefreshToken):
			return _badRefreshToken
		case "expired_token", "access_denied":
			return fmt.Errorf("%w: %s", ErrSignInEnded, failure.Error)
		}
		return fmt.Errorf("OAuth error: %s", failure.Error)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("decode OAuth response: %w", err)
	}
	return nil
}
