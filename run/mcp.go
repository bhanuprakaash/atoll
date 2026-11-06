package run

import (
	"context"
	"errors"
	"fmt"

	"atoll/mcpserver"
	"atoll/provider"
	"atoll/store"
)

// mcpServers returns the library servers the Bot picks, with their secrets, as the library stands
// now. A pick the library no longer has is skipped.
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
		out = append(out, u)
	}
	return out, nil
}
