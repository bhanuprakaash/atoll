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

const (
	_tail      = "[^\\s`'\")\\]]*"
	_nameChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
	_pathChars = _nameChars + "/~"
)

var (
	_unsafe   = regexp.MustCompile(`[^a-z0-9_-]+`)
	_safePath = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)
	// _homeRef finds `${HOME` followed by an operator, `$HOME`, and `~` with an optional user name,
	// `+` or `-`. Whether a `~` really starts a word is checked on the match.
	_homeRef = regexp.MustCompile(`\$\{HOME(?:\}|[^A-Za-z0-9_}\s])` + _tail +
		`|\$HOME\b(?:/` + _tail + `)?|~(?:[A-Za-z_][A-Za-z0-9_-]*|[+-])?(?:/` + _tail + `)?`)
)

// Import is a skill read from a local folder, with skipped files, refused references and rewrites.
type Import struct {
	Skill     store.Skill
	Skipped   []Skipped
	Refs      []HomeRef
	Rewritten []Rewrite
}

// Rewrite counts the own-folder references rewritten in one file of a skill.
type Rewrite struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

// HomeRef is a reference to the owner's home folder that a Bot cannot use.
type HomeRef struct {
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
	if err := readFolder(dir, &imp); err != nil {
		return Import{}, fmt.Errorf("read skill folder: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Import{}, err
	}
	forms := ownForms(entry, dir)
	scan := func(file string, content *string) {
		count := 0
		for _, form := range forms {
			var n int
			*content, n = replaceForm(*content, form)
			count += n
		}
		if count > 0 {
			imp.Rewritten = append(imp.Rewritten, Rewrite{File: file, Count: count})
		}
		imp.Refs = append(imp.Refs, homeRefs(file, *content, home)...)
	}
	scan("SKILL.md", &imp.Skill.Content)
	for i := range imp.Skill.Files {
		scan(imp.Skill.Files[i].Path, &imp.Skill.Files[i].Content)
	}
	slices.SortStableFunc(imp.Refs, func(a, b HomeRef) int { return strings.Compare(a.File, b.File) })
	slices.SortFunc(imp.Rewritten, func(a, b Rewrite) int { return strings.Compare(a.File, b.File) })
	return imp, nil
}

// readFolder adds the text files of dir to imp and lists what it leaves out.
func readFolder(dir string, imp *Import) error {
	err := filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error {
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
	slices.SortFunc(imp.Skill.Files, func(a, b store.SkillFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	return err
}

// ownForms are the spellings of a skill's own folder: the entry it was imported from and the
// resolved folder, as absolute paths and as `~/`, `$HOME/` and `${HOME}/` paths under their root.
func ownForms(entry, dir string) []string {
	forms := []string{entry, dir}
	root := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(filepath.Dir(entry))), "skills")) + "/"
	for _, name := range []string{filepath.Base(entry), filepath.Base(dir)} {
		for _, prefix := range []string{"~/", "$HOME/", "${HOME}/"} {
			forms = append(forms, prefix+root+name)
		}
	}
	return forms
}

// replaceForm replaces each whole-name occurrence of form in s with {{ATOLL_SKILL_DIR}}. A `~/` or
// absolute form does not count when a path character comes before it (`a~/x`, `/mnt/home/...`).
func replaceForm(s, form string) (string, int) {
	var out strings.Builder
	count, pos := 0, 0
	for {
		i := strings.Index(s[pos:], form)
		if i < 0 {
			break
		}
		start := pos + i
		end := start + len(form)
		own := (form[0] != '~' && form[0] != '/' || startsWord(s, start)) &&
			(end == len(s) || !strings.ContainsRune(_nameChars, rune(s[end])))
		out.WriteString(s[pos:start])
		if own {
			out.WriteString("{{ATOLL_SKILL_DIR}}")
			count++
		} else {
			out.WriteString(form)
		}
		pos = end
	}
	if count == 0 {
		return s, 0
	}
	out.WriteString(s[pos:])
	return out.String(), count
}

// startsWord reports whether no path character comes before s[i].
func startsWord(s string, i int) bool {
	return i == 0 || strings.IndexByte(_pathChars, s[i-1]) < 0
}

// homeRefs lists the home references left in content, in order of line, then position.
func homeRefs(file, content, home string) []HomeRef {
	var refs []HomeRef
	for n, line := range strings.Split(content, "\n") {
		var spans [][]int
		for _, loc := range _homeRef.FindAllStringIndex(line, -1) {
			if line[loc[0]] != '~' || startsWord(line, loc[0]) && tildeEnds(line, loc) {
				spans = append(spans, loc)
			}
		}
		spans = append(spans, absoluteRefs(line, home)...)
		slices.SortFunc(spans, func(a, b []int) int { return a[0] - b[0] })
		last := 0
		for _, sp := range spans {
			if sp[0] >= last {
				refs = append(refs, HomeRef{File: file, Line: n + 1, Ref: line[sp[0]:sp[1]]})
				last = sp[1]
			}
		}
	}
	return refs
}

// tildeEnds reports whether a `~` match is more than a bare `~` or ends the word: `~5` and `~~` are not.
func tildeEnds(line string, loc []int) bool {
	return loc[1]-loc[0] > 1 || loc[1] == len(line) || strings.ContainsRune(" \t\r\f:)\"'", rune(line[loc[1]]))
}

// absoluteRefs finds home as a whole path: no path character before it, and `/`, the end,
// whitespace or a quote after it.
func absoluteRefs(line, home string) [][]int {
	var spans [][]int
	for pos := 0; ; {
		i := strings.Index(line[pos:], home)
		if i < 0 {
			return spans
		}
		start := pos + i
		end := start + len(home)
		pos = end
		if !startsWord(line, start) || end < len(line) && !strings.ContainsRune("/ \t\r\f\"'`", rune(line[end])) {
			continue
		}
		if end < len(line) && line[end] == '/' {
			for end < len(line) && !strings.ContainsRune(" \t\r\f`'\")]", rune(line[end])) {
				end++
			}
		}
		spans = append(spans, []int{start, end})
	}
}

// Safe reports whether a shell leaves path alone: no space, quote, `$`, backtick or other
// character it splits or expands.
func Safe(path string) bool {
	return _safePath.MatchString(path)
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
