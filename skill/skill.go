// Package skill places a Bot's skills where a provider discovers them for one run.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"atoll/store"
)

var _unsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

// Place writes read-only copies of skills into dir, a new folder that belongs to one run, and
// returns the root to hand the provider: Claude reads dir/.claude/skills, Codex reads dir. With no
// skills it writes nothing and returns "".
func Place(dir, providerName string, skills []store.Skill) (string, error) {
	if len(skills) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", fmt.Errorf("make skill parent: %w", err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("make skill dir: %w", err)
	}
	root := dir
	if providerName == "claude" {
		root = filepath.Join(dir, ".claude", "skills")
	}
	used := map[string]bool{}
	for _, s := range skills {
		// Claude invokes a skill by its folder name.
		name := _unsafe.ReplaceAllString(strings.ToLower(s.Name), "-")
		if name == "" || used[name] {
			name = s.ID
		}
		used[name] = true
		folder := filepath.Join(root, name)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return "", fmt.Errorf("make skill folder: %w", err)
		}
		content := Strip(s.Content, providerName)
		if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(content), 0o444); err != nil {
			return "", fmt.Errorf("write skill %s: %w", s.ID, err)
		}
	}
	return dir, nil
}

// Strip returns the skill text a provider gets. A Bot cannot type slash commands, so
// disable-model-invocation would only hide the skill: every provider's copy drops its line.
// allowed-tools and context are Claude invocation controls, so Codex copies drop those too; every
// other byte stays.
func Strip(content, providerName string) string {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return content
	}
	codex := providerName == "codex"
	var out strings.Builder
	inFront, dropping, closed := false, false, false
	for line := range strings.Lines(content) {
		text := strings.TrimRight(line, "\r\n")
		switch {
		case closed:
		case !inFront:
			inFront = true
		case text == "---":
			closed = true
		case strings.HasPrefix(text, "disable-model-invocation:") ||
			codex && (strings.HasPrefix(text, "allowed-tools:") || strings.HasPrefix(text, "context:")):
			dropping = true
			continue
		case dropping && (strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t") ||
			strings.HasPrefix(text, "- ")):
			continue
		default:
			dropping = false
		}
		out.WriteString(line)
	}
	if !closed {
		return content
	}
	return out.String()
}
