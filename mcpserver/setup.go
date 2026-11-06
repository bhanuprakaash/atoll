package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"atoll/store"

	"github.com/zalando/go-keyring"
)

// SlackID is the catalog server that signs in with a Client ID the user saves, because Slack
// registers no clients.
const SlackID = "slack"

// Scopes returns the scopes the resource metadata of s lists, which its Client ID's app needs.
func Scopes(ctx context.Context, c *http.Client, s store.MCPServer) ([]string, error) {
	meta, err := discover(ctx, c, s.URL)
	if err != nil {
		return nil, fmt.Errorf("find sign-in of %s: %w", s.Name, err)
	}
	return meta.Scopes, nil
}

// SaveClientID keeps the Client ID of server id in the OS secret store, drops the tokens the old
// app issued and marks the server not connected.
func SaveClientID(ctx context.Context, st *store.Store, id, clientID string) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return invalid("client_id is required")
	}
	if err := keyring.Set("atoll", clientIDUser(id), clientID); err != nil {
		return fmt.Errorf("save Client ID of MCP server %s: %w", id, err)
	}
	if err := deleteTokens(id); err != nil {
		return err
	}
	return st.SetMCPSignIn(ctx, id, store.MCPNotConnected)
}

func savedClientID(id string) (string, error) {
	v, err := keyring.Get("atoll", clientIDUser(id))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNeedsSetup
	}
	if err != nil {
		return "", fmt.Errorf("read Client ID of MCP server %s: %w", id, err)
	}
	return v, nil
}

func clientIDUser(id string) string { return "mcp:" + id + ":client_id" }
