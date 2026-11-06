// Package skill places a Bot's skills where a provider discovers them for one run, and reads a
// skill from a local folder.
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"atoll/store"
)

// Reasons a file of a local skill folder is not imported.
const (
	SkippedGit    = "git"
	SkippedBinary = "binary"
	SkippedOther  = "not a file"
)

var (
	_unsafe    = regexp.MustCompile(`[^a-z0-9_-]+`)
	_homePaths = regexp.MustCompile("~/[^\\s`'\")\\]]*")
)

// Import is a skill read from a local folder: what was taken, what was left out and why, and the
// home-relative paths still in its SKILL.md.
type Import struct {
	Skill     store.Skill
	Skipped   []Skipped
	HomePaths []string
}

// Skipped is a file or folder of a local skill that was not imported.
type Skipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

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
		for _, f := range s.Files {
			file := filepath.Join(folder, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				return "", fmt.Errorf("make skill folder: %w", err)
			}
			mode := os.FileMode(0o444)
			if f.Executable {
				mode = 0o555
			}
			if err := os.WriteFile(file, []byte(f.Content), mode); err != nil {
				return "", fmt.Errorf("write skill %s file %s: %w", s.ID, f.Path, err)
			}
		}
	}
	return dir, nil
}

// Check returns why files cannot be a skill's files: a path that is not relative and clean, is
// SKILL.md, repeats, or is both a file and a folder; or content that is not text.
func Check(files []store.SkillFile) error {
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		switch {
		case !fs.ValidPath(f.Path) || f.Path == "." || f.Path == "SKILL.md":
			return fmt.Errorf("invalid file path %q", f.Path)
		case paths[f.Path]:
			return fmt.Errorf("file %q appears twice", f.Path)
		case !isText([]byte(f.Content)):
			return fmt.Errorf("file %q is not text", f.Path)
		}
		paths[f.Path] = true
	}
	for p := range paths {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if paths[dir] {
				return fmt.Errorf("%q is both a file and a folder", dir)
			}
		}
	}
	return nil
}

// Read imports the skill in the local folder dir without writing to it. It takes SKILL.md and every
// other text file, skips binaries and .git, and makes ~/.claude/skills/<folder>/ in SKILL.md
// relative to the skill. Name is SKILL.md's frontmatter name, else the folder name.
func Read(dir string) (Import, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Import{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	if !isText(data) {
		return Import{}, errors.New("SKILL.md is not text")
	}
	content := strings.ReplaceAll(string(data),
		"~/.claude/skills/"+filepath.Base(dir)+"/", "")
	imp := Import{Skill: store.Skill{
		Name: frontmatterName(content), Content: content, Source: dir,
	}}
	if imp.Skill.Name == "" {
		imp.Skill.Name = filepath.Base(dir)
	}
	for _, p := range _homePaths.FindAllString(content, -1) {
		if !slices.Contains(imp.HomePaths, p) {
			imp.HomePaths = append(imp.HomePaths, p)
		}
	}
	err = filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.Name() == ".git":
			imp.Skipped = append(imp.Skipped, Skipped{Path: rel, Reason: SkippedGit})
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		case d.IsDir() || rel == "SKILL.md":
			return nil
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			imp.Skipped = append(imp.Skipped, Skipped{Path: rel, Reason: SkippedOther})
			return nil
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !isText(data) {
			imp.Skipped = append(imp.Skipped, Skipped{Path: rel, Reason: SkippedBinary})
			return nil
		}
		imp.Skill.Files = append(imp.Skill.Files, store.SkillFile{
			Path: rel, Content: string(data), Executable: info.Mode()&0o111 != 0,
		})
		return nil
	})
	if err != nil {
		return Import{}, fmt.Errorf("read skill folder: %w", err)
	}
	slices.SortFunc(imp.Skill.Files, func(a, b store.SkillFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	return imp, nil
}

// Hash identifies a skill's SKILL.md and files, whatever order the files come in.
func Hash(sk store.Skill) string {
	files := slices.Clone(sk.Files)
	slices.SortFunc(files, func(a, b store.SkillFile) int { return strings.Compare(a.Path, b.Path) })
	h := sha256.New()
	h.Write([]byte(strconv.Quote(sk.Content)))
	for _, f := range files {
		_, _ = fmt.Fprintf(h, "\n%q %t %q", f.Path, f.Executable, f.Content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Diff lists the paths next adds to, changes in and removes from prev. SKILL.md counts as a path.
func Diff(prev, next store.Skill) (added, changed, removed []string) {
	added, changed, removed = []string{}, []string{}, []string{}
	if prev.Content != next.Content {
		changed = append(changed, "SKILL.md")
	}
	old := make(map[string]store.SkillFile, len(prev.Files))
	for _, f := range prev.Files {
		old[f.Path] = f
	}
	for _, f := range next.Files {
		o, ok := old[f.Path]
		switch {
		case !ok:
			added = append(added, f.Path)
		case o != f:
			changed = append(changed, f.Path)
		}
		delete(old, f.Path)
	}
	for _, f := range prev.Files {
		if _, ok := old[f.Path]; ok {
			removed = append(removed, f.Path)
		}
	}
	slices.Sort(changed)
	return added, changed, removed
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

func frontmatterName(content string) string {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return ""
	}
	for i, line := range slices.Collect(strings.Lines(content)) {
		text := strings.TrimRight(line, "\r\n")
		if i > 0 && text == "---" {
			break
		}
		if name, ok := strings.CutPrefix(text, "name:"); ok {
			return strings.Trim(strings.TrimSpace(name), `"'`)
		}
	}
	return ""
}

func isText(data []byte) bool {
	return utf8.Valid(data) && !slices.Contains(data, 0)
}
