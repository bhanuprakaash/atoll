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

// ErrPending is returned by PollToken while the user has not approved the sign-in yet.
var ErrPending = errors.New("authorization pending")

// Device is a started device-flow sign-in: the user enters UserCode at VerificationURI.
type Device struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
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

// PollToken asks once for the access token of the sign-in deviceCode. It returns ErrPending
// until the user approves.
func PollToken(ctx context.Context, c *http.Client, instance, deviceCode string) (string, error) {
	var reply struct {
		AccessToken string `json:"access_token"`
	}
	err := post(ctx, c, instance, "/oauth/token", url.Values{
		"client_id": {ClientID}, "device_code": {deviceCode},
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

// SaveToken stores the token of connection id in the OS secret store.
func SaveToken(id, token string) error {
	return keyring.Set("atoll", "gitlab:"+id, token)
}

// Token reads the token of connection id from the OS secret store.
func Token(id string) (string, error) {
	return keyring.Get("atoll", "gitlab:"+id)
}

// DeleteToken removes the token of connection id from the OS secret store.
func DeleteToken(id string) error {
	return keyring.Delete("atoll", "gitlab:"+id)
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
			if resp.StatusCode == http.StatusBadRequest && (reply.Error == "authorization_pending" || reply.Error == "slow_down") {
				return ErrPending
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
