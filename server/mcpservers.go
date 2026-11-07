package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"atoll/connection"
	"atoll/mcpserver"
	"atoll/provider"
	"atoll/store"
)

// mcpLibrary adds the GitHub connection's row while that connection exists.
func (a *api) mcpLibrary(ctx context.Context) ([]store.MCPServer, error) {
	servers, err := mcpserver.List(ctx, a.st)
	if err != nil {
		return nil, err
	}
	ok, err := a.hasGitHubConnection(ctx)
	if err != nil || !ok {
		return servers, err
	}
	row := store.MCPServer{
		ID: provider.GitHubMCPServer, Name: "GitHub", Kind: store.MCPConnection,
		URL: connection.GitHubMCPURL(),
	}
	i := slices.IndexFunc(servers, func(s store.MCPServer) bool { return s.Kind != store.MCPCatalog })
	if i < 0 {
		i = len(servers)
	}
	return slices.Insert(servers, i, row), nil
}

func (a *api) hasGitHubConnection(ctx context.Context) (bool, error) {
	conns, err := a.st.Connections(ctx)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(conns, func(c store.Connection) bool {
		return c.Kind == store.KindGitHub && c.Status != store.ConnectionPending
	}), nil
}

func (a *api) refuseGitHubRow(w http.ResponseWriter, r *http.Request) bool {
	if r.PathValue("id") != provider.GitHubMCPServer {
		return false
	}
	ok, err := a.hasGitHubConnection(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return true
	}
	if ok {
		fail(w, http.StatusBadRequest, "GitHub comes from its connection and cannot be changed here")
	}
	return ok
}

func (a *api) listMCPServers(w http.ResponseWriter, r *http.Request) {
	servers, err := a.mcpLibrary(r.Context())
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
	s, err := mcpserver.Add(r.Context(), a.st, a.http, in)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, s)
}

func (a *api) putMCPServer(w http.ResponseWriter, r *http.Request) {
	if a.refuseGitHubRow(w, r) {
		return
	}
	in, ok := readMCPServer(w, r)
	if !ok {
		return
	}
	s, err := mcpserver.Replace(r.Context(), a.st, a.http, r.PathValue("id"), in)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, s)
}

func (a *api) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	if a.refuseGitHubRow(w, r) {
		return
	}
	if err := mcpserver.Remove(r.Context(), a.st, r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) connectMCPServer(w http.ResponseWriter, r *http.Request) {
	s, err := mcpserver.Get(r.Context(), a.st, r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	redirect := "http://127.0.0.1:" + localPort(r) + "/oauth/callback"
	p, err := mcpserver.Begin(r.Context(), a.http, s, redirect)
	if errors.Is(err, mcpserver.ErrNeedsApp) {
		if s.Status != store.MCPNeedsApp {
			if err := a.st.SetMCPSignIn(r.Context(), s.ID, store.MCPNeedsApp); err != nil {
				a.fail(w, r, err)
				return
			}
		}
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.mu.Lock()
	for state, old := range a.signIns {
		if time.Since(old.Started) > _signInExpiry {
			delete(a.signIns, state)
		}
	}
	a.signIns[p.State] = p
	a.mu.Unlock()
	write(w, http.StatusOK, struct {
		AuthorizeURL string `json:"authorize_url"`
	}{AuthorizeURL: p.AuthorizeURL})
}

func (a *api) oauthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	a.mu.Lock()
	p, ok := a.signIns[state]
	delete(a.signIns, state)
	a.mu.Unlock()
	code := r.URL.Query().Get("code")
	switch {
	case !ok:
		callbackPage(w, http.StatusBadRequest, "This sign-in link is not valid or was already used.")
	case code == "":
		callbackPage(w, http.StatusBadRequest, p.Server.Name+" was not allowed. Close this window and try again.")
	default:
		err := mcpserver.Finish(r.Context(), a.http, a.st, p, code)
		if errors.Is(err, mcpserver.ErrInvalid) {
			callbackPage(w, http.StatusBadRequest, p.Server.Name+" was removed or changed. Close this window and connect again.")
			return
		}
		if err != nil {
			a.log.Error("MCP sign-in failed", "server", p.Server.Name, "err", err)
			callbackPage(w, http.StatusBadGateway, "Sign-in to "+p.Server.Name+" failed. Close this window and try again.")
			return
		}
		callbackPage(w, http.StatusOK, "Signed in to "+p.Server.Name+". You can close this window.")
	}
}

func callbackPage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><title>Reef</title><p>%s</p>", html.EscapeString(msg))
}

func localPort(r *http.Request) string {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return ""
	}
	_, port, _ := net.SplitHostPort(addr.String())
	return port
}

// The name is a key in a provider's config: Atoll's own server and the GitHub connection's names
// are taken.
func readMCPServer(w http.ResponseWriter, r *http.Request) (mcpserver.Input, bool) {
	var in mcpserver.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return in, false
	}
	if in.Name == provider.MCPServer || in.Name == provider.GitHubMCPServer {
		fail(w, http.StatusBadRequest, fmt.Sprintf("the name %q is reserved for Reef", in.Name))
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
