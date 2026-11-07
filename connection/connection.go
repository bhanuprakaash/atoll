// Package connection signs in to external services and keeps their tokens in the OS secret store.
package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	// DefaultInstance is the GitLab instance used when none is given.
	DefaultInstance = "https://gitlab.com"
	// ClientID is atoll's public, non-confidential GitLab OAuth app on gitlab.com.
	ClientID = "e95073653d459b416b4eaea3cf11696c43044a8313e0b3029cf05ca8e92e6c08"
	_scope   = "api write_repository"
)

var (
	// ErrPending is returned by PollToken while the user has not approved the sign-in yet.
	ErrPending = errors.New("authorization pending")
	// ErrSlowDown is ErrPending for a poll that came too early: every later poll must wait 5s more.
	ErrSlowDown = fmt.Errorf("%w: slow down", ErrPending)
	// ErrNeedsSignIn is returned when GitLab rejects a refresh token.
	ErrNeedsSignIn = errors.New("GitLab needs sign-in")
)

const _refreshBefore = 10 * time.Minute

// Device is a started device-flow sign-in: the user enters UserCode at VerificationURI.
type Device struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// Tokens is the refreshable OAuth token record saved for a GitLab connection.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// StartDevice starts a GitLab OAuth device-flow sign-in at instance.
func StartDevice(ctx context.Context, c *http.Client, instance string) (Device, error) {
	var reply struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	err := post(ctx, c, instance, "/oauth/authorize_device", url.Values{
		"client_id": {ClientID}, "scope": {_scope},
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

// PollToken asks once for the tokens of the sign-in deviceCode. It returns ErrPending
// until the user approves.
func PollToken(ctx context.Context, c *http.Client, instance, deviceCode string) (Tokens, error) {
	var reply struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	err := post(ctx, c, instance, "/oauth/token", url.Values{
		"client_id": {ClientID}, "device_code": {deviceCode},
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
	}, &reply)
	if err != nil {
		return Tokens{}, err
	}
	if reply.AccessToken == "" || reply.RefreshToken == "" || reply.ExpiresIn <= 0 {
		return Tokens{}, errors.New("OAuth reply has incomplete tokens")
	}
	return Tokens{
		AccessToken: reply.AccessToken, RefreshToken: reply.RefreshToken,
		ExpiresAt: time.Now().UTC().Add(time.Duration(reply.ExpiresIn) * time.Second),
	}, nil
}

// SaveTokens stores the OAuth tokens of connection id in the OS secret store.
func SaveTokens(id string, tokens Tokens) error {
	b, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("encode OAuth tokens: %w", err)
	}
	return keyring.Set("atoll", "gitlab:"+id, string(b))
}

// UsableToken loads a connection token and refreshes it when it expires soon. A pre-refresh
// bare token is accepted as-is because its expiry is unknown.
func UsableToken(ctx context.Context, c *http.Client, instance, id string) (string, error) {
	value, err := keyring.Get("atoll", "gitlab:"+id)
	if err != nil {
		return "", err
	}
	var tokens Tokens
	if json.Unmarshal([]byte(value), &tokens) != nil {
		return value, nil
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.ExpiresAt.IsZero() {
		return "", errors.New("stored OAuth tokens are incomplete")
	}
	if time.Until(tokens.ExpiresAt) > _refreshBefore {
		return tokens.AccessToken, nil
	}
	refreshed, err := refresh(ctx, c, instance, tokens.RefreshToken)
	if errors.Is(err, _invalidGrant) {
		return "", ErrNeedsSignIn
	}
	if err != nil {
		return "", err
	}
	if err := SaveTokens(id, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

// DeleteToken removes the token of connection id from the OS secret store.
func DeleteToken(id string) error {
	return keyring.Delete("atoll", "gitlab:"+id)
}

type _oauthError string

func (e _oauthError) Error() string { return string(e) }

const _invalidGrant _oauthError = "invalid_grant"

func refresh(ctx context.Context, c *http.Client, instance, refreshToken string) (Tokens, error) {
	var reply struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	err := post(ctx, c, instance, "/oauth/token", url.Values{
		"client_id": {ClientID}, "refresh_token": {refreshToken}, "grant_type": {"refresh_token"},
	}, &reply)
	if err != nil {
		return Tokens{}, err
	}
	if reply.AccessToken == "" || reply.RefreshToken == "" || reply.ExpiresIn <= 0 {
		return Tokens{}, errors.New("OAuth refresh reply has incomplete tokens")
	}
	return Tokens{
		AccessToken: reply.AccessToken, RefreshToken: reply.RefreshToken,
		ExpiresAt: time.Now().UTC().Add(time.Duration(reply.ExpiresIn) * time.Second),
	}, nil
}

func post(ctx context.Context, c *http.Client, instance, path string, form url.Values, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(instance, "/")+path, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create OAuth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("send OAuth request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		var reply struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&reply); err == nil && reply.Error != "" {
			if resp.StatusCode == http.StatusBadRequest && reply.Error == "authorization_pending" {
				return ErrPending
			}
			if resp.StatusCode == http.StatusBadRequest && reply.Error == "slow_down" {
				return ErrSlowDown
			}
			if resp.StatusCode == http.StatusBadRequest && reply.Error == "invalid_grant" {
				return _invalidGrant
			}
			return fmt.Errorf("OAuth error: %s", reply.Error)
		}
		return fmt.Errorf("OAuth HTTP status: %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode OAuth response: %w", err)
	}
	return nil
}
