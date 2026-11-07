package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

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

// GitHubURL is the base URL of GitHub's sign-in and the one git's credential helper is scoped
// to: ATOLL_GITHUB_URL, else github.com.
func GitHubURL() string {
	if u := os.Getenv("ATOLL_GITHUB_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return GitHubInstance
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
func PollGitHubToken(ctx context.Context, c *http.Client, deviceCode string) (string, error) {
	var reply struct {
		AccessToken string `json:"access_token"`
	}
	err := githubPost(ctx, c, "/login/oauth/access_token", url.Values{
		"client_id": {GitHubClientID}, "device_code": {deviceCode},
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
	}, &reply)
	if err != nil {
		return "", err
	}
	if reply.AccessToken == "" {
		return "", errors.New("OAuth reply has no access token")
	}
	return reply.AccessToken, nil
}

// CheckGitHubToken returns ErrGitHubNeedsSignIn when GitHub rejects the token.
func CheckGitHubToken(ctx context.Context, c *http.Client, token string) error {
	api := _githubAPI
	if u := os.Getenv("ATOLL_GITHUB_URL"); u != "" {
		api = GitHubURL()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"/user", nil)
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
		return ErrGitHubNeedsSignIn
	}
	return fmt.Errorf("GitHub HTTP status: %d", resp.StatusCode)
}

// SaveGitHubToken stores the token in the OS secret store.
func SaveGitHubToken(id, token string) error {
	return keyring.Set("atoll", "github:"+id, token)
}

// GitHubToken loads the token from the OS secret store.
func GitHubToken(id string) (string, error) {
	return keyring.Get("atoll", "github:"+id)
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
		return fmt.Errorf("send OAuth request: %w", err)
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
		}
		return fmt.Errorf("OAuth error: %s", failure.Error)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("decode OAuth response: %w", err)
	}
	return nil
}
