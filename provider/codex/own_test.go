package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOwn(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		skills     []string
		wantMCP    []string
		wantSkills []string
	}{
		{"nothing there", "", nil, []string{}, []string{}},
		{
			"servers and skills",
			"model = \"x\"\n[mcp_servers.zed]\ncommand = \"z\"\n[mcp_servers.zed.env]\nK = \"v\"\n" +
				"[mcp_servers.\"my figma\"]\nurl = \"u\"\n",
			[]string{"pdf", ".system", "art"}, []string{"my figma", "zed"}, []string{"art", "pdf"},
		},
		{"not TOML", "[mcp_servers", nil, []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.config != "" {
				if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, s := range tt.skills {
				if err := os.MkdirAll(filepath.Join(home, ".codex", "skills", s), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := Own(home)
			if !reflect.DeepEqual(got.MCPServers, tt.wantMCP) || !reflect.DeepEqual(got.Skills, tt.wantSkills) {
				t.Errorf("Own = %+v, want servers %v skills %v", got, tt.wantMCP, tt.wantSkills)
			}
		})
	}
}
