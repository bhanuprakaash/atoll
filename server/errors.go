package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"atoll/mcpserver"
	"atoll/memory"
	"atoll/run"
	"atoll/store"
)

const (
	_internalSentence = "Something went wrong on our side."
	_notFoundSentence = "That no longer exists."
)

// fail answers a failed request {"error": "<plain sentence>", "code": "<id>"}: the code is the
// status's own, and a 404 or 500 gets its fixed sentence, never msg.
func fail(w http.ResponseWriter, status int, msg string) {
	switch status {
	case http.StatusBadRequest:
		failCode(w, status, "invalid", msg)
	case http.StatusUnauthorized, http.StatusForbidden:
		failCode(w, status, "forbidden", "That request wasn't allowed.")
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		failCode(w, status, "not_found", _notFoundSentence)
	case http.StatusConflict:
		failCode(w, status, "conflict", msg)
	case http.StatusNotImplemented:
		failCode(w, status, "unsupported", msg)
	default:
		failCode(w, status, "internal", _internalSentence)
	}
}

// Fail is fail for the handlers outside this package; err is logged, never sent.
func Fail(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	fail(w, status, msg)
}

func failCode(w http.ResponseWriter, status int, code, msg string) {
	write(w, status, struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{Error: msg, Code: code})
}

// fail answers err with the status and code it maps to. Anything else is internal: its chain goes
// to the log, never to the client.
func (a *api) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, run.ErrNotFound),
		errors.Is(err, memory.ErrNotFound):
		fail(w, http.StatusNotFound, "")
	case errors.Is(err, memory.ErrInvalid):
		fail(w, http.StatusBadRequest, unprefixed(err, memory.ErrInvalid))
	case errors.Is(err, mcpserver.ErrInvalid):
		fail(w, http.StatusBadRequest, unprefixed(err, mcpserver.ErrInvalid))
	case errors.Is(err, run.ErrNotOffered):
		fail(w, http.StatusBadRequest, "Allow in this thread isn't offered for this approval.")
	case errors.Is(err, store.ErrEmpty):
		fail(w, http.StatusBadRequest, "Write a message or add an image.")
	case errors.Is(err, store.ErrSent):
		fail(w, http.StatusConflict, "That message was already sent.")
	case errors.Is(err, run.ErrBusy):
		failCode(w, http.StatusConflict, "busy", "A run is going in this thread.")
	case errors.Is(err, run.ErrClosed):
		failCode(w, http.StatusConflict, "closed", "This thread is closed.")
	case errors.Is(err, store.ErrExists):
		failCode(w, http.StatusConflict, "exists", "Something with that name already exists.")
	case errors.Is(err, store.ErrUndone):
		fail(w, http.StatusConflict, "That change was already undone.")
	case errors.Is(err, store.ErrInUse):
		fail(w, http.StatusConflict, "A thread still uses that.")
	case errors.Is(err, store.ErrPinLimit):
		fail(w, http.StatusConflict, store.ErrPinLimit.Error())
	case errors.Is(err, mcpserver.ErrKeychain):
		a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		failKeychain(w, "that MCP server's secrets")
	default:
		a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		fail(w, http.StatusInternalServerError, "")
	}
}

func failKeychain(w http.ResponseWriter, what string) {
	failCode(w, http.StatusInternalServerError, "keychain", "Reef couldn't save "+what+" to the system keychain.")
}

// unprefixed is err's text from the rule on: a package's name for its sentinel is not for people.
func unprefixed(err, sentinel error) string {
	text, prefix := err.Error(), sentinel.Error()+": "
	if i := strings.LastIndex(text, prefix); i >= 0 {
		return text[i+len(prefix):]
	}
	return text
}
