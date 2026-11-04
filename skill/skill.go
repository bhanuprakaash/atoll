// Package skill places a Bot's skills where a provider discovers them for one run.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"atoll/store"
)

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
	for _, s := range skills {
		folder := filepath.Join(root, s.ID)
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

// Strip returns the skill text a provider gets. Codex rejects the frontmatter keys allowed-tools
// and context, so their lines go; every other byte stays.
func Strip(content, providerName string) string {
	if providerName != "codex" || !strings.HasPrefix(content, "---\n") {
		return content
	}
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
		case strings.HasPrefix(text, "allowed-tools:") || strings.HasPrefix(text, "context:"):
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
