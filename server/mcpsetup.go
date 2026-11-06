package server

import (
	"encoding/json"
	"net/http"

	"atoll/mcpserver"
	"atoll/store"
)

// redirectURI is the one fixed URL an authorization server sends the browser back to.
func redirectURI(r *http.Request) string {
	return "http://127.0.0.1:" + localPort(r) + "/oauth/callback"
}

// mcpServerSetup answers what the user needs to create the app whose Client ID the server signs in
// with: the redirect URL to register and the scopes to grant.
func (a *api) mcpServerSetup(w http.ResponseWriter, r *http.Request) {
	s, ok := a.setupServer(w, r)
	if !ok {
		return
	}
	scopes, err := mcpserver.Scopes(r.Context(), a.http, s)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, struct {
		RedirectURI string   `json:"redirect_uri"`
		Scopes      []string `json:"scopes"`
	}{RedirectURI: redirectURI(r), Scopes: orEmpty(scopes)})
}

func (a *api) saveMCPServerSetup(w http.ResponseWriter, r *http.Request) {
	s, ok := a.setupServer(w, r)
	if !ok {
		return
	}
	var in struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := mcpserver.SaveClientID(r.Context(), a.st, s.ID, in.ClientID); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setupServer is the server of the request when it has a Client ID to set up, else it answers 404.
func (a *api) setupServer(w http.ResponseWriter, r *http.Request) (s store.MCPServer, ok bool) {
	s, err := mcpserver.Get(r.Context(), a.st, r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return s, false
	}
	if s.ID != mcpserver.SlackID {
		fail(w, http.StatusNotFound, "MCP server "+s.Name+" has nothing to set up")
		return s, false
	}
	return s, true
}
