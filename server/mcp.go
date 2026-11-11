package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"atoll/memory"
	"atoll/provider"
	"atoll/run"
	"atoll/store"
)

// JSON-RPC 2.0 error codes.
const (
	_parseError     = -32700
	_methodNotFound = -32601
	_invalidParams  = -32602
	_internalError  = -32603
)

const _tools = `{"tools":[
{"name":"finish_run",
"description":"Report how this run went. Call it once in every run, when the work is done and before your final reply.",
"inputSchema":{"type":"object",
"properties":{"done":{"type":"string"},"tried":{"type":"string"},"blocked":{"type":"string","description":"What stopped the work that only the user can fix: a missing sign-in or token, no access, a denied approval, a failing service. Empty string if nothing. A question you asked in your reply is not blocked."},"next":{"type":"string"}},
"required":["done","tried","blocked","next"]}},
{"name":"start_work",
"description":"Repo threads start in a clean read-only checkout. Call before your first edit in a repo thread. Makes this thread's worktree on a new branch type/slug and returns its path and branch as JSON. Change into that path for your edits. Calling it again returns the same worktree.",
"inputSchema":{"type":"object",
"properties":{"type":{"type":"string","enum":["feat","fix","chore","docs","test"]},"slug":{"type":"string"},"base":{"type":"string","description":"Optional plain branch name on origin, e.g. test/x"}},
"required":["type","slug"]}},
{"name":"track_mr",
"description":"When the change is ready, commit, push, open a merge request, call this tool with its URL and title, and include the URL in your reply. Never merge.",
"inputSchema":{"type":"object","properties":{"url":{"type":"string"},"title":{"type":"string"}},"required":["url","title"]}},
{"name":"approve",
"description":"Permission prompt: asks the user whether a tool call may go ahead.",
"inputSchema":{"type":"object",
"properties":{"tool_name":{"type":"string"},"input":{"type":"object"},"tool_use_id":{"type":"string"}},
"required":["tool_name","input"]}},
{"name":"remember",
"description":"Save one lasting fact for later threads: a decision, convention, finding, fact or lesson. Put one idea in body and the reason in why. Scope repo is for this repo, bot for this Bot only, user for the user in every repo. To change an older item, pass its id in replaces: it is retired and this one takes its place. Returns the saved item and a similar array of existing items that look the same.",
"inputSchema":{"type":"object",
"properties":{"kind":{"type":"string","enum":["decision","convention","finding","fact","lesson"]},"body":{"type":"string"},"why":{"type":"string"},"scope":{"type":"string","enum":["repo","user","bot"]},"replaces":{"type":"array","items":{"type":"string"}}},
"required":["kind","body","why","scope"]}},
{"name":"recall",
"description":"Search saved memory for this repo, this Bot and the user. Returns an array of items, best match first. Call read_memory with an id for one item.",
"inputSchema":{"type":"object",
"properties":{"query":{"type":"string"},"limit":{"type":"integer"}},
"required":["query"]}},
{"name":"read_memory",
"description":"Read one saved memory item by id, with its body and why.",
"inputSchema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}},
{"name":"retire_memory",
"description":"Retire a memory item that is wrong or out of date, by id. It stops appearing in recall and in prompts; its history stays. Returns the item.",
"inputSchema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}
]}`

type (
	rpcRequest struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}

	rpcResponse struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result,omitempty"`
		Error   *rpcError       `json:"error,omitempty"`
	}

	rpcError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
)

// mcp serves Atoll's MCP tools to the run whose token is in the path: streamable HTTP, one JSON
// reply per POST. approve blocks until the user answers, so the server sets no write timeout.
func (a *api) mcp(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodPost:
	default:
		w.Header().Set("Allow", "POST, DELETE")
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{Code: _parseError, Message: "parse error"},
		})
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	res := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	res.Result, res.Error = a.dispatch(r, req)
	write(w, http.StatusOK, res)
}

func (a *api) dispatch(r *http.Request, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid params"}
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "atoll", "version": "0"},
		}, nil
	case "tools/list":
		bot, thread, _, err := a.runs.Source(r.PathValue("token"))
		boss := err == nil && len(bot.Delegates) > 0 && thread.ParentThreadID == ""
		if err != nil || (!bot.CanManagePlatform && !boss) {
			return json.RawMessage(_tools), nil
		}
		var base struct {
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal([]byte(_tools), &base)
		if bot.CanManagePlatform {
			var extra []json.RawMessage
			_ = json.Unmarshal([]byte(_platformTools), &extra)
			base.Tools = append(base.Tools, extra...)
		}
		if boss {
			extra, err := a.delegateTools(r.Context(), bot)
			if err != nil {
				return nil, a.toolError(err)
			}
			base.Tools = append(base.Tools, extra...)
		}
		return base, nil
	case "tools/call":
		return a.callTool(r, req.Params)
	}
	return nil, &rpcError{Code: _methodNotFound, Message: "method not found: " + req.Method}
}

func (a *api) callTool(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &rpcError{Code: _invalidParams, Message: "invalid params"}
	}
	token := r.PathValue("token")
	if isPlatformTool(call.Name) {
		return a.platformCall(r.Context(), token, call.Name, call.Arguments)
	}
	switch call.Name {
	case provider.ToolFinishRun:
		var rep store.Report
		if err := json.Unmarshal(call.Arguments, &rep); err != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		if strings.TrimSpace(rep.Done) == "" {
			return failure("done is required."), nil
		}
		blocked := strings.TrimSpace(rep.Blocked)
		blocked = strings.TrimSpace(strings.TrimRightFunc(blocked, unicode.IsPunct))
		switch strings.ToLower(blocked) {
		case "nothing blocking", "none", "nothing", "n/a":
			rep.Blocked = ""
		}
		if err := a.runs.Report(r.Context(), token, rep); err != nil {
			return nil, a.toolError(err)
		}
		return text("Report saved."), nil
	case provider.ToolStartWork:
		var in struct {
			Type string `json:"type"`
			Slug string `json:"slug"`
			Base string `json:"base"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		path, branch, base, err := a.runs.StartWork(r.Context(), token, in.Type, in.Slug, in.Base)
		if errors.Is(err, run.ErrNoGitHubConnection) {
			return failure(err.Error()), nil
		}
		if err != nil {
			return nil, a.toolError(err)
		}
		body, err := json.Marshal(struct {
			Path   string `json:"path"`
			Branch string `json:"branch"`
			Base   string `json:"base,omitempty"`
			Note   string `json:"note,omitempty"`
		}{Path: path, Branch: branch, Base: base.Name, Note: base.Note})
		if err != nil {
			return nil, a.toolError(err)
		}
		return text(string(body)), nil
	case provider.ToolTrackMR:
		var in struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil || in.URL == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		if err := a.runs.TrackMR(r.Context(), token, in.URL, in.Title); err != nil {
			return a.refusal(err)
		}
		return text("MR tracked."), nil
	case provider.ToolRemember:
		var in struct {
			Kind     string   `json:"kind"`
			Body     string   `json:"body"`
			Why      string   `json:"why"`
			Scope    string   `json:"scope"`
			Replaces []string `json:"replaces"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		item, similar, err := a.runs.Remember(
			r.Context(), token, in.Kind, in.Body, in.Why, in.Scope, in.Replaces)
		if err != nil {
			return nil, a.toolError(err)
		}
		return a.jsonText(struct {
			memory.Item
			Similar []memory.Similar `json:"similar"`
		}{Item: item, Similar: orEmpty(similar)})
	case provider.ToolRecall:
		var in struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		items, err := a.runs.Recall(r.Context(), token, in.Query, in.Limit)
		if err != nil {
			return nil, a.toolError(err)
		}
		return a.jsonText(orEmpty(items))
	case provider.ToolReadMemory, provider.ToolRetireMemory:
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil || in.ID == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		read := a.runs.ReadMemory
		if call.Name == provider.ToolRetireMemory {
			read = a.runs.RetireMemory
		}
		item, err := read(r.Context(), token, in.ID)
		if err != nil {
			return nil, a.toolError(err)
		}
		return a.jsonText(item)
	case provider.ToolDelegate:
		var in struct {
			Bot   string `json:"bot"`
			Brief string `json:"brief"`
			Repo  string `json:"repo"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil || in.Bot == "" || in.Brief == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		h, err := a.runs.Delegate(r.Context(), token, in.Bot, in.Brief, in.Repo)
		if res, rpcErr := a.refusal(err); res != nil || rpcErr != nil {
			return res, rpcErr
		}
		return a.jsonText(h)
	case provider.ToolContinue:
		var in struct {
			Thread  string `json:"thread"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil || in.Thread == "" || in.Message == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		queuedFor, err := a.runs.Continue(r.Context(), token, in.Thread, in.Message)
		if res, rpcErr := a.refusal(err); res != nil || rpcErr != nil {
			return res, rpcErr
		}
		if queuedFor != "" {
			return text("Queued: " + queuedFor + " is working; it gets this when its run ends."), nil
		}
		return text("Sent."), nil
	case provider.ToolApprove:
		var in struct {
			ToolName  string          `json:"tool_name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
		}
		if err := json.Unmarshal(call.Arguments, &in); err != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		allow, err := a.runs.Ask(r.Context(), token, in.ToolUseID, in.ToolName, in.Input)
		if err != nil {
			return nil, a.toolError(err)
		}
		answer := struct {
			Behavior     string          `json:"behavior"`
			UpdatedInput json.RawMessage `json:"updatedInput,omitempty"`
			Message      string          `json:"message,omitempty"`
		}{Behavior: "deny", Message: "The user rejected this step."}
		if allow {
			answer.Behavior, answer.UpdatedInput, answer.Message = "allow", in.Input, ""
		}
		body, err := json.Marshal(answer)
		if err != nil {
			return nil, a.toolError(err)
		}
		return text(string(body)), nil
	}
	return nil, &rpcError{Code: _invalidParams, Message: "unknown tool: " + call.Name}
}

func (a *api) delegateTools(ctx context.Context, bot store.Bot) ([]json.RawMessage, error) {
	var names []string
	for _, id := range bot.Delegates {
		b, err := a.st.Bot(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		names = append(names, b.Name)
	}
	str := map[string]string{"type": "string"}
	tools := []map[string]any{{
		"name": provider.ToolDelegate,
		"description": "Hand work to a Bot on your delegate list (" + strings.Join(names, ", ") +
			"). Returns at once with {\"thread_id\"}; the helper works in a thread of its own and " +
			"its replies come back to you. The brief must hold everything the helper needs. " +
			"The helper works in this thread's repo; with no repo here, repo names a registered repo.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"bot": str, "brief": str, "repo": str},
			"required":   []string{"bot", "brief"},
		},
	}, {
		"name": provider.ToolContinue,
		"description": "Send a message to a helper thread you started with delegate, for more work " +
			"on the same task. Start a new delegate for different work.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"thread": str, "message": str},
			"required":   []string{"thread", "message"},
		},
	}}
	out := make([]json.RawMessage, 0, len(tools))
	for _, t := range tools {
		b, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func (a *api) refusal(err error) (any, *rpcError) {
	var refused run.RefusedError
	switch {
	case err == nil:
		return nil, nil
	case errors.As(err, &refused), errors.Is(err, run.ErrClosed):
		return failure(err.Error()), nil
	}
	return nil, a.toolError(err)
}

func (a *api) toolError(err error) *rpcError {
	if !errors.Is(err, run.ErrNotFound) && !errors.Is(err, memory.ErrNotFound) &&
		!errors.Is(err, memory.ErrInvalid) {
		a.log.Error("mcp tool failed", "err", err)
	}
	return &rpcError{Code: _internalError, Message: err.Error()}
}

func (a *api) jsonText(v any) (any, *rpcError) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, a.toolError(err)
	}
	return text(string(body)), nil
}

func text(s string) any { return result(s, false) }

func failure(s string) any { return result(s, true) }

func result(s string, isError bool) any {
	type part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	return struct {
		Content []part `json:"content"`
		IsError bool   `json:"isError,omitempty"`
	}{Content: []part{{Type: "text", Text: s}}, IsError: isError}
}
