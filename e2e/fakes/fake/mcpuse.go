package fake

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// MCPCall is one tool call of the mcp-use scenario: the prompt "Use MCP tools. " followed by a
// JSON array of {"server","tool","args"}. The fake calls each on the server of that name that its
// config holds, asking for permission first as the real CLI does for a server that is not Atoll's.
// The reply is one MCPLine per call, so a test sees what each call answered. With "schema" set the
// fake sends only arguments the tool's schema declares, as a model reading tools/list would, and
// answers "Error: <tool> declares no "<name>" parameter" for any other.
type MCPCall struct {
	Server string          `json:"server"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
	Schema bool            `json:"schema,omitempty"`
}

// Undeclared is the error text for the first argument of the call that c's schema for the tool does
// not declare, or "" when the call is not checked or every argument is declared.
func (call MCPCall) Undeclared(c *MCP) string {
	var args map[string]json.RawMessage
	if !call.Schema || json.Unmarshal(call.Args, &args) != nil {
		return ""
	}
	for name := range args {
		if !slices.Contains(c.Params[call.Tool], name) {
			return fmt.Sprintf("Error: %s declares no %q parameter: %q", call.Tool, name, c.Params[call.Tool])
		}
	}
	return ""
}

// MCPCalls reads the calls of an mcp-use prompt.
func MCPCalls(prompt string) ([]MCPCall, error) {
	_, payload, ok := strings.Cut(prompt, "Use MCP tools. ")
	if !ok {
		return nil, fmt.Errorf("mcp-use payload absent")
	}
	var calls []MCPCall
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &calls); err != nil {
		return nil, fmt.Errorf("mcp-use payload: %w", err)
	}
	for i := range calls {
		if len(calls[i].Args) == 0 {
			calls[i].Args = json.RawMessage(`{}`)
		}
	}
	return calls, nil
}

// MCPLine is what one call adds to the reply: "<server>/<tool>: <text>". The text is the tool's
// answer, "unavailable" when the config has no such server or the server does not list the tool,
// or "denied" when the user said no.
func MCPLine(c MCPCall, text string) string {
	return c.Server + "/" + c.Tool + ": " + text
}
