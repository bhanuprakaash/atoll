// Package server is Atoll's HTTP API: REST and a per-thread SSE stream under /api/, Atoll's MCP
// tools under /mcp/, and the UI at /.
package server

import (
	"log/slog"
	"net/http"

	"atoll/run"
	"atoll/store"
)

// MCPPath is where Atoll's MCP endpoint lives; a run's server is MCPPath + its token.
const MCPPath = "/mcp/"

// New returns the handler for the API, the MCP tools and the UI.
func New(st *store.Store, runs *run.Manager, log *slog.Logger) http.Handler {
	return http.NotFoundHandler()
}
