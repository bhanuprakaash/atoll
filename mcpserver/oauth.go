package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"atoll/store"

	"github.com/zalando/go-keyring"
)

const (
	_refreshBefore = 10 * time.Minute
	_probeTimeout  = 5 * time.Second
	_clientName    = "Reef"
	_initialize    = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` +
		`{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"Reef","version":"0"}}}`
)

var (
	// ErrNeedsApp is returned when a server cannot register clients, so it needs an app registered
	// by hand.
	ErrNeedsApp = errors.New("this server needs a registered app")
	// ErrNeedsSetup is returned when a server needs its Client ID saved before it can sign in.
	ErrNeedsSetup = errors.New("this server needs its Client ID: set it up first")
	// ErrNeedsSignIn is wrapped when a server's tokens are rejected: the user must sign in again.
	ErrNeedsSignIn = errors.New("needs sign-in")

	errNoSignIn     = errors.New("server does not ask for sign-in")
	errInvalidGrant = errors.New("invalid_grant")

	_resourceMetadata = regexp.MustCompile(`resource_metadata="([^"]+)"`)
)

// Tokens is the sign-in record of one server, kept in the OS secret store.
type Tokens struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"` // zero when the server gave no expiry
}

// Pending is a sign-in that waits for the user to allow it in the browser.
type Pending struct {
	Server        store.MCPServer
	State         string
	AuthorizeURL  string
	Verifier      string
	ClientID      string
	Redirect      string
	TokenEndpoint string
}

type metadata struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	Scopes                []string `json:"-"` // scopes_supported of the resource metadata
}

// Begin starts the sign-in of s: it finds the server's authorization server, registers a client
// whose redirect is redirect (Slack uses the Client ID the user saved, and asks for its scopes),
// and returns the URL the user opens to allow it.
func Begin(ctx context.Context, c *http.Client, s store.MCPServer, redirect string) (Pending, error) {
	switch s.Status {
	case "":
		return Pending{}, invalid(s.Name + " needs no sign-in")
	case store.MCPNeedsApp:
		return Pending{}, ErrNeedsApp
	case store.MCPNeedsSetup:
		return Pending{}, ErrNeedsSetup
	}
	meta, err := discover(ctx, c, s.URL)
	if errors.Is(err, errNoSignIn) {
		return Pending{}, invalid(s.Name + " needs no sign-in")
	}
	if err != nil {
		return Pending{}, fmt.Errorf("find sign-in of %s: %w", s.Name, err)
	}
	var clientID string
	switch {
	case s.ID == SlackID:
		clientID, err = savedClientID(s.ID)
	case meta.RegistrationEndpoint == "":
		return Pending{}, ErrNeedsApp
	default:
		clientID, err = register(ctx, c, meta.RegistrationEndpoint, redirect)
	}
	if err != nil {
		return Pending{}, fmt.Errorf("client of %s: %w", s.Name, err)
	}
	verifier, state := randomText(), randomText()
	sum := sha256.Sum256([]byte(verifier))
	u, err := url.Parse(meta.AuthorizationEndpoint)
	if err != nil {
		return Pending{}, fmt.Errorf("authorization endpoint of %s: %w", s.Name, err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirect)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("resource", s.URL)
	if s.ID == SlackID {
		q.Set("scope", strings.Join(meta.Scopes, " "))
	}
	u.RawQuery = q.Encode()
	return Pending{
		Server: s, State: state, AuthorizeURL: u.String(), Verifier: verifier, ClientID: clientID,
		Redirect: redirect, TokenEndpoint: meta.TokenEndpoint,
	}, nil
}

// Finish trades the code the user's Allow produced for tokens, stores them, and marks the server
// connected.
func Finish(ctx context.Context, c *http.Client, st *store.Store, p Pending, code string) error {
	tokens, err := postToken(ctx, c, p.TokenEndpoint, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.Redirect},
		"client_id": {p.ClientID}, "code_verifier": {p.Verifier}, "resource": {p.Server.URL},
	})
	if err != nil {
		return fmt.Errorf("sign in to %s: %w", p.Server.Name, err)
	}
	tokens.ClientID = p.ClientID
	if err := saveTokens(p.Server.ID, tokens); err != nil {
		return err
	}
	return st.SetMCPSignIn(ctx, p.Server.ID, store.MCPConnected)
}

// Token returns the access token of the signed-in server s, refreshed first when it expires
// within ten minutes. A rejected refresh marks the server needs_sign_in.
func Token(ctx context.Context, c *http.Client, st *store.Store, s store.MCPServer) (string, error) {
	if s.Status == store.MCPNeedsSignIn {
		return "", fmt.Errorf("%s %w", s.Name, ErrNeedsSignIn)
	}
	value, err := keyring.Get("atoll", tokensUser(s.ID))
	if err != nil {
		return "", fmt.Errorf("read sign-in of MCP server %s: %w", s.Name, err)
	}
	var tokens Tokens
	if err := json.Unmarshal([]byte(value), &tokens); err != nil {
		return "", fmt.Errorf("decode sign-in of MCP server %s: %w", s.Name, err)
	}
	if tokens.ExpiresAt.IsZero() || time.Until(tokens.ExpiresAt) > _refreshBefore {
		return tokens.AccessToken, nil
	}
	if tokens.RefreshToken == "" {
		return "", needsSignIn(ctx, st, s)
	}
	meta, err := discover(ctx, c, s.URL)
	if err != nil {
		return "", fmt.Errorf("find sign-in of %s: %w", s.Name, err)
	}
	refreshed, err := postToken(ctx, c, meta.TokenEndpoint, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken},
		"client_id": {tokens.ClientID}, "resource": {s.URL},
	})
	if errors.Is(err, errInvalidGrant) {
		return "", needsSignIn(ctx, st, s)
	}
	if err != nil {
		return "", fmt.Errorf("refresh sign-in of %s: %w", s.Name, err)
	}
	refreshed.ClientID = tokens.ClientID
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tokens.RefreshToken
	}
	if err := saveTokens(s.ID, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

// needsSignIn marks s needs_sign_in and returns the error that says so.
func needsSignIn(ctx context.Context, st *store.Store, s store.MCPServer) error {
	if err := st.SetMCPSignIn(ctx, s.ID, store.MCPNeedsSignIn); err != nil {
		return err
	}
	return fmt.Errorf("%s %w", s.Name, ErrNeedsSignIn)
}

// probe is the sign-in status a remote server at serverURL starts with: "" when it needs none or
// cannot be reached.
func probe(ctx context.Context, c *http.Client, serverURL string) string {
	ctx, cancel := context.WithTimeout(ctx, _probeTimeout)
	defer cancel()
	meta, err := discover(ctx, c, serverURL)
	switch {
	case err != nil:
		return ""
	case meta.RegistrationEndpoint == "":
		return store.MCPNeedsApp
	}
	return store.MCPNotConnected
}

// discover reads the authorization server metadata a server's 401 points to. It returns
// errNoSignIn when the server does not answer 401 with a resource_metadata URL.
func discover(ctx context.Context, c *http.Client, serverURL string) (metadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, strings.NewReader(_initialize))
	if err != nil {
		return metadata{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.Do(req)
	if err != nil {
		return metadata{}, fmt.Errorf("send request: %w", err)
	}
	_ = resp.Body.Close()
	found := _resourceMetadata.FindStringSubmatch(resp.Header.Get("WWW-Authenticate"))
	if resp.StatusCode != http.StatusUnauthorized || found == nil {
		return metadata{}, errNoSignIn
	}
	var resource struct {
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	if err := getJSON(ctx, c, found[1], &resource); err != nil {
		return metadata{}, err
	}
	if len(resource.AuthorizationServers) == 0 {
		return metadata{}, errors.New("resource metadata names no authorization server")
	}
	issuer, err := url.Parse(resource.AuthorizationServers[0])
	if err != nil {
		return metadata{}, fmt.Errorf("authorization server URL: %w", err)
	}
	wellKnown := issuer.Scheme + "://" + issuer.Host + "/.well-known/oauth-authorization-server" +
		strings.TrimRight(issuer.Path, "/")
	var meta metadata
	meta.Scopes = resource.ScopesSupported
	return meta, getJSON(ctx, c, wellKnown, &meta)
}

func register(ctx context.Context, c *http.Client, endpoint, redirect string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"client_name": _clientName, "redirect_uris": []string{redirect},
		"grant_types":    []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	})
	if err != nil {
		return "", fmt.Errorf("encode registration: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	var reply struct {
		ClientID string `json:"client_id"`
	}
	if err := doJSON(c, req, &reply); err != nil {
		return "", err
	}
	if reply.ClientID == "" {
		return "", errors.New("registration reply has no client_id")
	}
	return reply.ClientID, nil
}

func postToken(ctx context.Context, c *http.Client, endpoint string, form url.Values) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var reply struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		OK           *bool  `json:"ok"` // Slack answers a rejection with HTTP 200 and ok false
		Error        string `json:"error"`
	}
	if err := doJSON(c, req, &reply); err != nil {
		return Tokens{}, err
	}
	if reply.OK != nil && !*reply.OK {
		if reply.Error == "invalid_refresh_token" || reply.Error == errInvalidGrant.Error() {
			return Tokens{}, errInvalidGrant
		}
		return Tokens{}, fmt.Errorf("%s rejected the request: %s", req.URL.Host, reply.Error)
	}
	if reply.AccessToken == "" {
		return Tokens{}, errors.New("token reply has no access_token")
	}
	tokens := Tokens{AccessToken: reply.AccessToken, RefreshToken: reply.RefreshToken}
	if reply.ExpiresIn > 0 {
		tokens.ExpiresAt = time.Now().UTC().Add(time.Duration(reply.ExpiresIn) * time.Second)
	}
	return tokens, nil
}

func getJSON(ctx context.Context, c *http.Client, endpoint string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	return doJSON(c, req, result)
}

// doJSON sends req and decodes a 2xx JSON reply into result. An error reply is reduced to its
// OAuth error code, so no token reaches an error message.
func doJSON(c *http.Client, req *http.Request, result any) error {
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := io.LimitReader(resp.Body, 1<<20)
	if resp.StatusCode/100 != 2 {
		var reply struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(body).Decode(&reply) == nil && reply.Error == errInvalidGrant.Error() {
			return errInvalidGrant
		}
		return fmt.Errorf("%s answered %d", req.URL.Host, resp.StatusCode)
	}
	if err := json.NewDecoder(body).Decode(result); err != nil {
		return fmt.Errorf("decode reply of %s: %w", req.URL.Host, err)
	}
	return nil
}

func randomText() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokensUser(id string) string { return "mcp:" + id + ":oauth" }

func saveTokens(id string, tokens Tokens) error {
	b, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("encode sign-in: %w", err)
	}
	if err := keyring.Set("atoll", tokensUser(id), string(b)); err != nil {
		return fmt.Errorf("save sign-in of MCP server %s: %w", id, err)
	}
	return nil
}

func deleteTokens(id string) error {
	if err := keyring.Delete("atoll", tokensUser(id)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete sign-in of MCP server %s: %w", id, err)
	}
	return nil
}
