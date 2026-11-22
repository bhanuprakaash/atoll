package claude

import (
	"slices"
	"testing"
)

func TestKeychainArgs(t *testing.T) {
	tests := []struct {
		name string
		user string
		dir  string
		want []string
	}{
		{
			"user and folder", "jo", "/Users/jo/Library/Application Support/Atoll/providers/claude",
			[]string{"find-generic-password", "-a", "jo", "-w", "-s", "Claude Code-credentials-3d966291"},
		},
		{
			"a user name with other characters", "jo smith", "/Users/jo/Library/Application Support/Atoll/providers/claude",
			[]string{"find-generic-password", "-a", "claude-code-user", "-w", "-s", "Claude Code-credentials-3d966291"},
		},
		{
			"a decomposed folder name hashes as composed", "jo", "/Users/jö/x",
			[]string{"find-generic-password", "-a", "jo", "-w", "-s", "Claude Code-credentials-2153fda8"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("USER", tt.user)
			if got := keychainArgs(tt.dir); !slices.Equal(got, tt.want) {
				t.Errorf("keychainArgs(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}
