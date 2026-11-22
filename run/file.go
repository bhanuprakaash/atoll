package run

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"atoll/repo"
	"atoll/store"
)

const (
	_maxFileBytes = 1 << 20
	_sniffBytes   = 8 << 10
)

var (
	// ErrBinary is a file with a NUL byte in its first 8 KB.
	ErrBinary = errors.New("binary file")
	// ErrTooLarge is a file over 1 MB.
	ErrTooLarge = errors.New("file over 1 MB")
)

// File is a text file of a thread's checkout, or the tracked files a bare name matches.
type File struct {
	Path    string   `json:"path,omitempty"`
	Text    string   `json:"text"`
	Branch  string   `json:"branch,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Matches []string `json:"matches,omitempty"`
}

// ThreadPath resolves p in the thread's work folder, symlinks resolved. A relative p joins the
// folder; anything outside it, or missing, is ErrNotFound.
func (m *Manager) ThreadPath(t store.Thread, p string) (string, error) {
	root, err := filepath.EvalSymlinks(m.WorkDir(t))
	if err != nil {
		return "", ErrNotFound
	}
	return inCheckout(root, p)
}

func inCheckout(root, p string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	full, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", ErrNotFound
	}
	if rel, err := filepath.Rel(root, full); err != nil || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrNotFound
	}
	return full, nil
}

// ReadFile reads the text file p of the thread's checkout as it is now. A bare name not at the
// root resolves through the tracked files: one match is read, several come back as Matches. A
// thread with no repo, a closed thread and one whose worktree was removed have no file.
func (m *Manager) ReadFile(ctx context.Context, t store.Thread, p string) (File, error) {
	if t.RepoID == "" || t.Closed || t.WorktreeRemoved {
		return File{}, ErrNotFound
	}
	root, err := filepath.EvalSymlinks(m.WorkDir(t))
	if err != nil {
		return File{}, ErrNotFound
	}
	full, err := inCheckout(root, p)
	if errors.Is(err, ErrNotFound) && !strings.ContainsRune(p, '/') {
		found, terr := repo.Tracked(ctx, root, p)
		if terr != nil {
			return File{}, terr
		}
		switch len(found) {
		case 0:
		case 1:
			p = found[0]
			full, err = inCheckout(root, p)
		default:
			return File{Matches: found}, nil
		}
	}
	if err != nil {
		return File{}, err
	}
	text, err := readText(full)
	if err != nil {
		return File{}, err
	}
	if rel, err := filepath.Rel(root, full); err == nil {
		p = filepath.ToSlash(rel)
	}
	branch, commit := repo.Head(ctx, root)
	return File{Path: p, Text: text, Branch: branch, Commit: commit}, nil
}

func readText(full string) (string, error) {
	f, err := os.Open(full)
	if err != nil {
		return "", ErrNotFound
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return "", ErrNotFound
	}
	if info.Size() > _maxFileBytes {
		return "", ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, _maxFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > _maxFileBytes {
		return "", ErrTooLarge
	}
	if bytes.IndexByte(data[:min(len(data), _sniffBytes)], 0) >= 0 {
		return "", ErrBinary
	}
	return string(data), nil
}
