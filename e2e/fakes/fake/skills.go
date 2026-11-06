package fake

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type (
	// SkillCopy is a discovered file, never text supplied in the user prompt.
	SkillCopy struct {
		Name  string      `json:"name"`
		Path  string      `json:"path"`
		Text  string      `json:"text"`
		Files []SkillFile `json:"files,omitempty"`
	}

	// SkillFile is a file of a discovered skill's folder, SKILL.md included, as found on disk.
	SkillFile struct {
		Path string `json:"path"` // relative to the skill folder, slash-separated
		Mode uint32 `json:"mode"` // permission bits
		Text string `json:"text"`
	}
)

// SkillFiles reads every file under a skill folder, by path.
func SkillFiles(dir string) ([]SkillFile, error) {
	var files []SkillFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, SkillFile{Path: filepath.ToSlash(rel), Mode: uint32(info.Mode().Perm()), Text: string(b)})
		return nil
	})
	return files, err
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
			files, err := SkillFiles(filepath.Dir(path))
			if err != nil {
				return nil, err
			}
			found = append(found, SkillCopy{Name: name, Path: path, Text: text, Files: files})
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

// SkillsRun runs scripts/check of the discovered poteto-mode skill, in the folder the provider
// found it in, and names that folder in its reply. A prompt with "Wait, then" holds after
// discovery as HoldSkills does and logs the folder again once released, so a test sees what the
// live run still had.
func SkillsRun(skills []SkillCopy, prompt string) (string, error) {
	i := slices.IndexFunc(skills, func(s SkillCopy) bool { return s.Name == "poteto-mode" })
	if i < 0 {
		return "", fmt.Errorf("poteto-mode not discovered: %+v", skills)
	}
	dir := filepath.Dir(skills[i].Path)
	if strings.Contains(prompt, "Wait, then") {
		if err := HoldSkills(); err != nil {
			return "", err
		}
		files, err := SkillFiles(dir)
		if err != nil {
			return "", err
		}
		skills[i].Files = files
		if err := LogSkills(skills); err != nil {
			return "", err
		}
	}
	script := filepath.Join(dir, "scripts", "check")
	cmd := exec.Command(script)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run %s: %w", script, err)
	}
	return "Ran " + script + ": " + strings.TrimSpace(string(out)), nil
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
