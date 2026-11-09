// Package skill places a Bot's skills where a provider discovers them for one run, and reads a
// skill from a local folder.
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
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
	_unsafe = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// Import is a skill read from a local folder, with skipped files, refused references and rewrites.
type Import struct {
	Skill     store.Skill
	Skipped   []Skipped
	Refs      []homeRef
	Rewritten map[string]int
}

type homeRef struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Ref  string `json:"ref"`
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
		content := strings.ReplaceAll(Strip(s.Content, providerName), "{{ATOLL_SKILL_DIR}}", folder)
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
			if err := os.WriteFile(file, []byte(strings.ReplaceAll(f.Content, "{{ATOLL_SKILL_DIR}}", folder)), mode); err != nil {
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

// Read imports a skill without changing its source, replacing own-folder paths with {{ATOLL_SKILL_DIR}}.
func Read(dir string, source ...string) (Import, error) {
	entry := dir
	if len(source) > 0 {
		entry = source[0]
	}
	skillPath := filepath.Join(dir, "SKILL.md")
	data, _, err := readFile(skillPath)
	if err != nil {
		return Import{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	if !isText(data) {
		return Import{}, errors.New("SKILL.md is not text")
	}
	content := string(data)
	imp := Import{Skill: store.Skill{
		Name: frontmatterName(content), Content: content, Source: entry,
	}}
	if imp.Skill.Name == "" {
		imp.Skill.Name = filepath.Base(entry)
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
		info, err := os.Lstat(file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			imp.Skipped = append(imp.Skipped, Skipped{Path: rel, Reason: SkippedOther})
			return nil
		}
		data, info, err := readFile(file)
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

	home, err := os.UserHomeDir()
	if err != nil {
		return Import{}, err
	}
	forms := []string{entry, dir}
	root := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(filepath.Dir(entry))), "skills")) + "/"
	for _, name := range []string{filepath.Base(entry), filepath.Base(dir)} {
		for _, prefix := range []string{"~/", "$HOME/", "${HOME}/"} {
			forms = append(forms, prefix+root+name)
		}
	}
	imp.Rewritten = make(map[string]int)
	scan := func(file string, content *string) {
		for _, form := range forms {
			re := regexp.MustCompile(regexp.QuoteMeta(form) + `($|[^a-zA-Z0-9._-])`)
			matches := re.FindAllStringSubmatchIndex(*content, -1)
			for i := len(matches) - 1; i >= 0; i-- {
				start, end := matches[i][0], matches[i][2]
				if strings.HasPrefix(form, "~/") && start > 0 && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-/~", rune((*content)[start-1])) {
					continue
				}
				*content = (*content)[:start] + "{{ATOLL_SKILL_DIR}}" + (*content)[end:]
				imp.Rewritten[file]++
			}
		}
		re := regexp.MustCompile("~/[^\\s`'\"\\)\\]]*|\\$HOME(?:/[^\\s`'\"\\)\\]]*)?|\\$\\{HOME\\}(?:/[^\\s`'\"\\)\\]]*)?|" + regexp.QuoteMeta(home) + "(?:/[^\\s`'\"\\)\\]]*)?")
		for lineNo, line := range strings.Split(*content, "\n") {
			for _, loc := range re.FindAllStringIndex(line, -1) {
				ref := line[loc[0]:loc[1]]
				if strings.HasPrefix(ref, "~/") && loc[0] > 0 && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-/~", rune(line[loc[0]-1])) {
					continue
				}
				if ref == "$HOME" && loc[1] < len(line) && ((line[loc[1]] >= 'A' && line[loc[1]] <= 'Z') || (line[loc[1]] >= 'a' && line[loc[1]] <= 'z') || line[loc[1]] == '_' || (line[loc[1]] >= '0' && line[loc[1]] <= '9')) {
					continue
				}
				imp.Refs = append(imp.Refs, homeRef{File: file, Line: lineNo + 1, Ref: ref})
			}
		}
	}
	scan("SKILL.md", &imp.Skill.Content)
	for i := range imp.Skill.Files {
		scan(imp.Skill.Files[i].Path, &imp.Skill.Files[i].Content)
	}
	slices.SortStableFunc(imp.Refs, func(a, b homeRef) int { return strings.Compare(a.File, b.File) })
	return imp, nil
}

func readFile(path string) ([]byte, fs.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(f)
	return data, info, err
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
