package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"atoll/mcpserver"
	"atoll/provider"
)

func (a *api) listMCPServers(w http.ResponseWriter, r *http.Request) {
	servers, err := mcpserver.List(r.Context(), a.st)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, servers)
}

func (a *api) createMCPServer(w http.ResponseWriter, r *http.Request) {
	in, ok := readMCPServer(w, r)
	if !ok {
		return
	}
	s, err := mcpserver.Add(r.Context(), a.st, in)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, s)
}

func (a *api) putMCPServer(w http.ResponseWriter, r *http.Request) {
	in, ok := readMCPServer(w, r)
	if !ok {
		return
	}
	s, err := mcpserver.Replace(r.Context(), a.st, r.PathValue("id"), in)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, s)
}

func (a *api) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	if err := mcpserver.Remove(r.Context(), a.st, r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The name is a key in a provider's config: Atoll's own server's name is taken.
func readMCPServer(w http.ResponseWriter, r *http.Request) (mcpserver.Input, bool) {
	var in mcpserver.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return in, false
	}
	if in.Name == provider.MCPServer {
		fail(w, http.StatusBadRequest, fmt.Sprintf("the name %q is Reef's own server", in.Name))
		return in, false
	}
	return in, true
}

func (a *api) unknownMCPServers(ctx context.Context, picks []string) (string, error) {
	if len(picks) == 0 {
		return "", nil
	}
	servers, err := mcpserver.List(ctx, a.st)
	if err != nil {
		return "", err
	}
	ids := make([]string, len(servers))
	for i, s := range servers {
		ids[i] = s.ID
	}
	var unknown []string
	for _, id := range picks {
		if !slices.Contains(ids, id) {
			unknown = append(unknown, strconv.Quote(id))
		}
	}
	if len(unknown) == 0 {
		return "", nil
	}
	return fmt.Sprintf("unknown MCP server ids %s; MCP servers: %s",
		strings.Join(unknown, ", "), strings.Join(ids, ", ")), nil
}
