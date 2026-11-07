package mcpserver

import (
	"errors"
	"testing"

	"atoll/store"
)

func TestPlanName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"plain", "my-server_1", nil},
		{"space", "my server", ErrInvalid},
		{"double underscore", "atoll__evil", ErrInvalid},
		{"catalog name", "context7", store.ErrExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Input{Name: tt.input, Command: "x"}.plan(store.MCPServer{})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("plan(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
