package fake

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// SkillCopy is a discovered file, never text supplied in the user prompt.
type SkillCopy struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Text string `json:"text"`
}

// DiscoverSkills models the verified provider search roots. Extra roots contain skill
// folders on Codex and .claude/skills on Claude; neither is a prompt fallback.
func DiscoverSkills(provider, cwd string, extra []string, disabled []string) ([]SkillCopy, error) {
	roots := []string{}
	if provider == "claude" {
		roots = append(roots, filepath.Join(ConfigDir("CLAUDE_CONFIG_DIR", ".claude"), "skills"))
		for _, p := range extra {
			roots = append(roots, filepath.Join(p, ".claude", "skills"))
		}
	} else {
		roots = append(roots, filepath.Join(ConfigDir("CODEX_HOME", ".codex"), "skills"), filepath.Join(os.Getenv("HOME"), ".agents", "skills"))
		roots = append(roots, extra...)
	}
	repoDir := ".agents"
	if provider == "claude" {
		repoDir = ".claude"
	}
	// Repo discovery walks ancestors up to the Git boundary, including linked worktrees.
	for dir := cwd; dir != ""; dir = filepath.Dir(dir) {
		roots = append(roots, filepath.Join(dir, repoDir, "skills"))
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil || filepath.Dir(dir) == dir {
			break
		}
	}
	var found []SkillCopy
	for _, root := range roots {
		paths, err := filepath.Glob(filepath.Join(root, "*", "SKILL.md"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if slices.Contains(disabled, path) {
				continue
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			text := string(b)
			parts := strings.SplitN(text, "---", 3)
			if len(parts) != 3 || parts[0] != "" {
				continue
			}
			name, description := "", ""
			for line := range strings.Lines(parts[1]) {
				key, value, _ := strings.Cut(line, ":")
				switch strings.TrimSpace(key) {
				case "name":
					name = strings.Trim(strings.TrimSpace(value), `"'`)
				case "description":
					description = strings.TrimSpace(value)
				}
			}
			if name == "" || description == "" {
				continue
			}
			found = append(found, SkillCopy{Name: name, Path: path, Text: text})
		}
	}
	slices.SortFunc(found, func(a, b SkillCopy) int { return strings.Compare(a.Name, b.Name) })
	return found, nil
}

// SkillsReply reads the discovered file again, exercising the content that a CLI uses.
func SkillsReply(skills []SkillCopy) (string, error) {
	names := []string{}
	for _, s := range skills {
		names = append(names, s.Name)
	}
	for _, s := range skills {
		if s.Name != "reef-greeting" {
			continue
		}
		b, err := os.ReadFile(s.Path)
		if err != nil {
			return "", err
		}
		if string(b) != s.Text {
			return "", fmt.Errorf("skill changed during active run: %s", s.Path)
		}
		_, body, ok := strings.Cut(strings.TrimPrefix(string(b), "---"), "---")
		if !ok {
			return "", fmt.Errorf("missing frontmatter")
		}
		return "Skills: " + strings.Join(names, ", ") + ". Used reef-greeting: " + strings.TrimSpace(body), nil
	}
	return "Skills: " + strings.Join(names, ", ") + ". No reef-greeting skill available.", nil
}

// LogSkills records the actual discovery, including file bytes, separately from prompts.
func LogSkills(skills []SkillCopy) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(exe+".log.skills", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return json.NewEncoder(f).Encode(struct {
		PID    int         `json:"pid"`
		Skills []SkillCopy `json:"skills"`
	}{os.Getpid(), skills})
}

// HoldSkills lets the test overlap another run and an edit after discovery but before use.
func HoldSkills() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ready := fmt.Sprintf("%s.ready.%d", exe, os.Getpid())
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(ready) }()
	for {
		if _, err := os.Stat(ready + ".release"); err == nil {
			return os.Remove(ready + ".release")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// WorktreeSkills follows the real start_work result before discovering repo skills.
func WorktreeSkills(c *MCP, provider string, initial []SkillCopy) ([]SkillCopy, error) {
	if c == nil {
		return nil, fmt.Errorf("atoll MCP server absent")
	}
	text, bad, err := c.Call("start_work", json.RawMessage(`{"type":"fix","slug":"skill-discovery"}`))
	if err != nil {
		return nil, err
	}
	if bad {
		return nil, fmt.Errorf("start_work: %s", text)
	}
	var work struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(text), &work); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(work.Path) {
		return nil, fmt.Errorf("start_work path: %q", work.Path)
	}
	found, err := DiscoverSkills(provider, work.Path, nil, nil)
	if err != nil {
		return nil, err
	}
	for _, s := range initial {
		if s.Name == "reef-greeting" {
			found = append(found, s)
		}
	}
	slices.SortFunc(found, func(a, b SkillCopy) int { return strings.Compare(a.Name, b.Name) })
	if err := LogSkills(found); err != nil {
		return nil, err
	}
	return found, nil
}
