// Package repo runs the git work behind a registered repo: worktrees, branch names and
// snapshots of unfinished work.
package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const _maxSlug = 40

var (
	// ErrType means the branch type is not one of feat, fix, chore or docs.
	ErrType = errors.New("type must be feat, fix, chore or docs")

	_nonSlug = regexp.MustCompile(`[^a-z0-9]+`)
)

// Branch normalises a branch name: ("fix", "Seeded BUG") -> "fix/seeded-bug". The slug is
// lowercased, every run of characters outside [a-z0-9] becomes "-", the ends are trimmed of "-"
// and the slug is cut to 40 characters.
func Branch(typ, slug string) (string, error) {
	switch typ {
	case "feat", "fix", "chore", "docs":
	default:
		return "", fmt.Errorf("branch type %q: %w", typ, ErrType)
	}
	s := Slug(slug)
	if s == "" {
		return "", errors.New("branch slug is empty")
	}
	return typ + "/" + s, nil
}

// Slug lowercases s, turns runs of characters outside [a-z0-9] into "-" and cuts it to 40.
func Slug(s string) string {
	s = strings.Trim(_nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > _maxSlug {
		s = strings.TrimRight(s[:_maxSlug], "-")
	}
	return s
}

// Base says where a new branch started. Name is the ref and short commit, such as
// "origin/HEAD ee6ad1b". Note is set when the local default branch has commits origin/HEAD lacks.
type Base struct {
	Name string
	Note string
}

type gitContextKey struct{}
type snapshotKey struct{}
type gitDirs struct{ common, dir string }

// WorktreeID validates the Git metadata for an existing worktree.
func WorktreeID(common, worktree string) (string, error) {
	data, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		return "", fmt.Errorf("read worktree git file: %w", err)
	}
	path := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
	if !filepath.IsAbs(path) {
		path = filepath.Join(worktree, path)
	}
	root, err := filepath.EvalSymlinks(filepath.Join(common, "worktrees"))
	if err != nil {
		return "", fmt.Errorf("resolve worktree git root: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil || filepath.Dir(path) != root {
		return "", errors.New("worktree git directory is outside the registered repo")
	}
	back, err := os.ReadFile(filepath.Join(path, "gitdir"))
	if err != nil {
		return "", fmt.Errorf("read worktree git backlink: %w", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(worktree, ".git"))
	if err != nil {
		return "", fmt.Errorf("resolve worktree git file: %w", err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(back)))
	if err != nil || got != want {
		return "", errors.New("worktree git backlink does not match")
	}
	return filepath.Base(path), nil
}

// WithWorktree uses the stored ID because worktree gitfiles and metadata are Bot-writable.
func WithWorktree(ctx context.Context, common, id string) context.Context {
	return context.WithValue(ctx, gitContextKey{}, gitDirs{common: common, dir: filepath.Join(common, "worktrees", id)})
}

// Create makes a new worktree of repoPath on a new branch at <root>/<repoName>/<folder>, where
// folder is the branch with "/" replaced by "-". When the branch or the folder exists it tries
// <branch>-2, <branch>-3 and so on. The branch starts at origin/HEAD when that resolves, else at
// HEAD. It returns the absolute path, the branch used and the base. No fetch, no network.
func Create(
	ctx context.Context, repoPath, root, repoName, branch string,
) (path, used string, base Base, err error) {
	start := "HEAD"
	if _, err := git(ctx, repoPath, nil, "rev-parse", "--verify", "--quiet", "origin/HEAD^{commit}"); err == nil {
		start = "origin/HEAD"
	}
	commit, err := git(ctx, repoPath, nil, "rev-parse", "--short=7", start)
	if err != nil {
		return "", "", Base{}, fmt.Errorf("read base: %w", err)
	}
	base = Base{Name: start + " " + commit}
	if start == "origin/HEAD" {
		base.Note = aheadNote(ctx, repoPath)
	}
	path, used, err = add(ctx, repoPath, root, repoName, branch, start)
	if err == nil && Origin(ctx, repoPath) != "" {
		for key, value := range map[string]string{"remote": "origin", "merge": "refs/heads/" + used} {
			if _, err = git(ctx, repoPath, nil, "config", "branch."+used+"."+key, value); err != nil {
				err = errors.Join(fmt.Errorf("set upstream: %w", err), Remove(ctx, repoPath, path), DeleteBranch(ctx, repoPath, used))
				return "", "", Base{}, err
			}
		}
	}
	return path, used, base, err
}

// CreateFrom is Create for a branch that starts at the local branch from.
func CreateFrom(
	ctx context.Context, repoPath, root, repoName, branch, from string,
) (path, used string, err error) {
	return add(ctx, repoPath, root, repoName, branch, "refs/heads/"+from)
}

func add(ctx context.Context, repoPath, root, repoName, branch, start string) (path, used string, err error) {
	for n := 1; ; n++ {
		used = branch
		if n > 1 {
			used = branch + "-" + strconv.Itoa(n)
		}
		path, err = filepath.Abs(filepath.Join(root, repoName, strings.ReplaceAll(used, "/", "-")))
		if err != nil {
			return "", "", fmt.Errorf("worktree path: %w", err)
		}
		if _, err := git(ctx, repoPath, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+used); err == nil {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			continue
		}
		break
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", fmt.Errorf("make worktree folder: %w", err)
	}
	if _, err := git(ctx, repoPath, nil, "worktree", "add", "--no-track", "-b", used, path, start); err != nil {
		return "", "", fmt.Errorf("add worktree: %w", err)
	}
	return path, used, nil
}

// aheadNote names the local default branch (the one origin/HEAD points at) and how far it is ahead
// of origin/HEAD. It is empty when it is not ahead or cannot be read.
func aheadNote(ctx context.Context, repoPath string) string {
	remote, err := git(ctx, repoPath, nil, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	local := strings.TrimPrefix(remote, "origin/")
	count, err := git(ctx, repoPath, nil, "rev-list", "--count", "origin/HEAD..refs/heads/"+local)
	if err != nil || count == "0" {
		return ""
	}
	tip, err := git(ctx, repoPath, nil, "rev-parse", "--short=7", "refs/heads/"+local)
	if err != nil {
		return ""
	}
	noun := "commits"
	if count == "1" {
		noun = "commit"
	}
	return fmt.Sprintf("local %s is %s %s ahead (%s); not included", local, count, noun, tip)
}

// Checkout makes dir a detached worktree of repoPath at HEAD. A dir that already is a worktree
// is left alone.
func Checkout(ctx context.Context, repoPath, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("make checkout folder: %w", err)
	}
	if _, err := git(ctx, repoPath, nil, "worktree", "add", "--detach", dir, "HEAD"); err != nil {
		return fmt.Errorf("add checkout: %w", err)
	}
	return nil
}

// Save stores HEAD plus every tracked change and untracked file (ignored ones left out) of the
// checkout at dir as a commit at ref. The index, HEAD and the files are not touched: the tree is
// built in a temporary index that starts as a copy of the real one. It returns false and writes
// nothing when the checkout is clean.
func Save(ctx context.Context, dir, ref string) (bool, error) {
	snapshot := context.WithValue(ctx, snapshotKey{}, true)
	status, err := git(snapshot, dir, nil, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("read status: %w", err)
	}
	if status == "" {
		return false, nil
	}
	head, err := git(ctx, dir, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return false, fmt.Errorf("read HEAD: %w", err)
	}
	tmp, err := os.MkdirTemp("", "atoll-save-")
	if err != nil {
		return false, fmt.Errorf("make temp index: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	index := filepath.Join(tmp, "index")
	if err := seedIndex(ctx, dir, index); err != nil {
		return false, err
	}
	env := []string{
		"GIT_INDEX_FILE=" + index,
		"GIT_AUTHOR_NAME=Atoll", "GIT_AUTHOR_EMAIL=atoll@localhost",
		"GIT_COMMITTER_NAME=Atoll", "GIT_COMMITTER_EMAIL=atoll@localhost",
	}
	if _, err := git(snapshot, dir, env, "add", "-A"); err != nil {
		return false, fmt.Errorf("stage changes: %w", err)
	}
	tree, err := git(snapshot, dir, env, "write-tree")
	if err != nil {
		return false, fmt.Errorf("write tree: %w", err)
	}
	commit, err := git(ctx, dir, env, "commit-tree", tree, "-p", head, "-m", "atoll unfinished work")
	if err != nil {
		return false, fmt.Errorf("commit tree: %w", err)
	}
	var refEnv []string
	if trusted, ok := ctx.Value(gitContextKey{}).(gitDirs); ok {
		// Git may follow the Bot-writable commondir for refs even with GIT_COMMON_DIR set.
		refEnv = []string{"GIT_DIR=" + trusted.common}
	}
	if _, err := git(ctx, dir, refEnv, "update-ref", ref, commit); err != nil {
		return false, fmt.Errorf("write ref: %w", err)
	}
	return true, nil
}

// Remove force-removes the worktree at dir of repoPath and keeps its branch. A missing dir is
// not an error.
func Remove(ctx context.Context, repoPath, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return nil
	}
	if _, err := git(context.WithoutCancel(ctx), repoPath, nil, "worktree", "remove", "--force", dir); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	return nil
}

// DeleteBranch force-deletes branch of repoPath.
func DeleteBranch(ctx context.Context, repoPath, branch string) error {
	if _, err := git(context.WithoutCancel(ctx), repoPath, nil, "branch", "-D", branch); err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}
	return nil
}

// IsRepo reports whether path is the top level of a git work tree.
func IsRepo(ctx context.Context, path string) bool {
	top, err := git(ctx, path, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	got, err := filepath.EvalSymlinks(top)
	return err == nil && got == want
}

// Origin is empty when the repo has no origin remote.
func Origin(ctx context.Context, path string) string {
	url, err := git(ctx, path, nil, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return url
}

// HasCommit reports whether the repo at path has a commit to branch from.
func HasCommit(ctx context.Context, path string) bool {
	_, err := git(ctx, path, nil, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// GitCommonDir returns the common git directory.
func GitCommonDir(ctx context.Context, path string) (string, error) {
	return git(ctx, path, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// WorktreeConfig reports whether Git uses per-worktree configuration.
func WorktreeConfig(ctx context.Context, path string) bool {
	value, err := git(ctx, path, nil, "config", "--type=bool", "--default=false", "--get", "extensions.worktreeConfig")
	return err != nil || value == "true"
}

func seedIndex(ctx context.Context, dir, to string) error {
	from, err := git(ctx, dir, nil, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return fmt.Errorf("find index: %w", err)
	}
	data, err := os.ReadFile(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		return fmt.Errorf("copy index: %w", err)
	}
	return nil
}

func git(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	// Hooks and fsmonitor can run commands from Bot-writable repo config.
	base := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", dir}
	trusted, hasTrusted := ctx.Value(gitContextKey{}).(gitDirs)
	allEnv := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if hasTrusted {
		allEnv = append(allEnv, "GIT_DIR="+trusted.dir, "GIT_COMMON_DIR="+trusted.common)
	}
	allEnv = append(allEnv, env...)
	if enabled, _ := ctx.Value(snapshotKey{}).(bool); enabled {
		config := exec.CommandContext(ctx, "git", append(base, "config", "--local", "--includes", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)...)
		config.Env = allEnv
		keys, err := config.Output()
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			return "", fmt.Errorf("list repo filters: %w", err)
		}
		count := 0
		seen := map[string]bool{}
		for _, key := range strings.Split(string(keys), "\x00") {
			if key != "" {
				name := key[:strings.LastIndexByte(key, '.')]
				if seen[name] {
					continue
				}
				seen[name] = true
				for _, setting := range [][2]string{{"clean", ""}, {"smudge", ""}, {"process", ""}, {"required", "false"}} {
					allEnv = append(allEnv, "GIT_CONFIG_KEY_"+strconv.Itoa(count)+"="+name+"."+setting[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(count)+"="+setting[1])
					count++
				}
			}
		}
		allEnv = append(allEnv, "GIT_CONFIG_COUNT="+strconv.Itoa(count))
	}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = allEnv
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}
