package server

import (
	"encoding/json"
	"testing"

	"atoll/run"
)

func TestNeedsYou(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{"claude write", "Write", `{"file_path":"/w/x"}`, "Wants to edit x"},
		{"claude edit", "Edit", `{"file_path":"/w/x"}`, "Wants to edit x"},
		{"claude bash", "Bash", `{"command":"make check"}`, "Wants to run make check"},
		{"claude bash with description", "Bash", `{"command":"make check","description":"Run the checks"}`, "Wants to run make check"},
		{"codex read", "commandExecution", `{"commandActions":[{"type":"read","path":"/w/x"}]}`, "Wants to read x"},
		{"codex search", "commandExecution", `{"commandActions":[{"type":"search","query":"TODO"}]}`, "Wants to search TODO"},
		{"mcp tool", "mcp__github__create_issue", `{}`, "Wants to use GitHub: create issue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsYou(run.Approval{Tool: tt.tool, Input: json.RawMessage(tt.input)})
			if got != tt.want {
				t.Errorf("needsYou = %q, want %q", got, tt.want)
			}
		})
	}
}
