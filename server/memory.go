package server

import (
	"encoding/json"
	"net/http"

	"atoll/memory"
)

func (a *api) createMemory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind    string `json:"kind"`
		Body    string `json:"body"`
		Why     string `json:"why"`
		Scope   string `json:"scope"`
		ScopeID string `json:"scope_id"`
		Ticket  string `json:"ticket"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	item, _, err := a.mem.Remember(r.Context(), memory.Note{
		Kind: body.Kind, Body: body.Body, Why: body.Why, Scope: body.Scope, ScopeID: body.ScopeID, Ticket: body.Ticket,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, item)
}

func (a *api) listMemory(w http.ResponseWriter, r *http.Request) {
	items, err := a.mem.List(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, orEmpty(items))
}

func (a *api) retireMemory(w http.ResponseWriter, r *http.Request) {
	if _, err := a.mem.Retire(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
