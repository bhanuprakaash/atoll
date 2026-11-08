package claude

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
			`{"mcpServers":{"zed":{"command":"z"},"figma":{"url":"u"}},"projects":{"/x":{"mcpServers":{"hidden":{}}}}}`,
			[]string{"pdf", ".system", "art"}, []string{"figma", "zed"}, []string{"art", "pdf"},
		},
		{"not JSON", `{`, nil, []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.config != "" {
				if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, s := range tt.skills {
				if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", s), 0o755); err != nil {
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
