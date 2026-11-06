package run

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"atoll/mcpserver"
	"atoll/provider"
	"atoll/store"
)

// mcpServers skips picks the library no longer has.
func (m *Manager) mcpServers(ctx context.Context, bot store.Bot) ([]provider.UserMCPServer, error) {
	var out []provider.UserMCPServer
	for _, id := range bot.MCPServers {
		s, err := mcpserver.Get(ctx, m.cfg.Store, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load MCP server: %w", err)
		}
		secrets, err := mcpserver.Secrets(s)
		if err != nil {
			return nil, err
		}
		u := provider.UserMCPServer{Name: s.Name, Command: s.Command, Args: s.Args, URL: s.URL}
		if s.Kind == store.MCPLocal {
			u.Env = secrets
		} else {
			u.Headers = secrets
		}
		if s.Kind != store.MCPLocal && (s.Status == store.MCPConnected || s.Status == store.MCPNeedsSignIn) {
			token, err := mcpserver.Token(ctx, &http.Client{Timeout: 30 * time.Second}, m.cfg.Store, s)
			if err != nil {
				return nil, err
			}
			u.Headers["Authorization"] = "Bearer " + token
		}
		out = append(out, u)
	}
	return out, nil
}
