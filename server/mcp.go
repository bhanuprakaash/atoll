package server

import (
	"encoding/json"
	"errors"
	"net/http"

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
"description":"Report how this run went. Call it once, at the end of every run, after your reply is ready.",
"inputSchema":{"type":"object",
"properties":{"done":{"type":"string"},"tried":{"type":"string"},"blocked":{"type":"string"},"next":{"type":"string"}},
"required":["done","tried","blocked","next"]}},
{"name":"approve",
"description":"Permission prompt: asks the user whether a tool call may go ahead.",
"inputSchema":{"type":"object",
"properties":{"tool_name":{"type":"string"},"input":{"type":"object"},"tool_use_id":{"type":"string"}},
"required":["tool_name","input"]}}
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
		return json.RawMessage(_tools), nil
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
	switch call.Name {
	case provider.ToolFinishRun:
		var rep store.Report
		if err := json.Unmarshal(call.Arguments, &rep); err != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid arguments"}
		}
		if err := a.runs.Report(r.Context(), token, rep); err != nil {
			return nil, a.toolError(err)
		}
		return text("Report saved."), nil
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

func (a *api) toolError(err error) *rpcError {
	if !errors.Is(err, run.ErrNotFound) {
		a.log.Error("mcp tool failed", "err", err)
	}
	return &rpcError{Code: _internalError, Message: err.Error()}
}

func text(s string) any {
	type part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	return struct {
		Content []part `json:"content"`
	}{[]part{{"text", s}}}
}
