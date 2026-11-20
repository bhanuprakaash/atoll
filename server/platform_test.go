package server

import (
	"encoding/json"
	"testing"

	"atoll/run"
)

func TestNeedsYou(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{"claude write", "Write", `{"file_path":"/w/x"}`, "Wants to edit x"},
		{"claude edit", "Edit", `{"file_path":"/w/x"}`, "Wants to edit x"},
		{"claude bash", "Bash", `{"command":"make check"}`, "Wants to run make check"},
		{"codex read", "commandExecution", `{"commandActions":[{"type":"read","path":"/w/x"}]}`, "Wants to read x"},
		{"codex search", "commandExecution", `{"commandActions":[{"type":"search","query":"TODO"}]}`, "Wants to search TODO"},
		{"mcp tool", "mcp__srv__tool", `{}`, "Wants to use mcp__srv__tool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := needsYou(run.Approval{Tool: tc.tool, Input: json.RawMessage(tc.input)})
			if got != tc.want {
				t.Errorf("needsYou = %q, want %q", got, tc.want)
			}
		})
	}
}
