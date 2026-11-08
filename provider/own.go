package provider

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Own names what a provider's own, non-Reef setup holds. Bots never use it.
type Own struct {
	MCPServers []string
	Skills     []string
}

// SkillNames returns the sorted names of the skill folders in dir, or none when dir is missing.
func SkillNames(dir string) []string {
	names := []string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() &&
			!strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names
}
