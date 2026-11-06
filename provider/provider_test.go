package provider

import (
	"slices"
	"strings"
	"testing"
)

func TestEnv(t *testing.T) {
	tests := []struct {
		name string
		set  string
		kept bool
	}{
		{"ANTHROPIC_API_KEY", "a", false},
		{"ANTHROPIC_BASE_URL", "b", false},
		{"CLAUDECODE", "1", false},
		{"CLAUDE_CODE_OAUTH_TOKEN", "c", false},
		{"CLAUDE_CONFIG_DIR", "d", false},
		{"OPENAI_API_KEY", "e", false},
		{"CODEX_SANDBOX", "g", false},
		{"GITLAB_TOKEN", "t", false},
		{"GH_TOKEN", "t", false},
		{"GITHUB_TOKEN", "t", false},
		{"GH_ENTERPRISE_TOKEN", "t", false},
		{"GITHUB_ENTERPRISE_TOKEN", "t", false},
		{"OAUTH_TOKEN", "t", false},
		{"GITLAB_HOST", "https://gitlab.example", false},
		{"GIT_CONFIG_PARAMETERS", "'credential.helper=x'", false},
		{"SSH_AUTH_SOCK", "/s", false},
		{"HOME", "/h", true},
		{"PATH", "/p", true},
		{"MY_CLAUDE", "1", true},
		{"ENABLE_CLAUDEAI_MCP_SERVERS", "true", true},
	}
	for _, tt := range tests {
		t.Setenv(tt.name, tt.set)
	}

	got := Env("CODEX_HOME=/own")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if in := slices.Contains(got, tt.name+"="+tt.set); in != tt.kept {
				t.Errorf("%s in Env = %v, want %v", tt.name, in, tt.kept)
			}
		})
	}
	if !slices.Contains(got, "CODEX_HOME=/own") {
		t.Errorf("Env lacks the appended CODEX_HOME=/own: %s", strings.Join(got, " "))
	}
}
