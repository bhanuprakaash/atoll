// Package repo runs the git work behind a registered repo: worktrees, branch names and
// snapshots of unfinished work.
package repo

import (
	"bytes"
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

var _nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Branch normalises a branch name: ("fix", "Seeded BUG") -> "fix/seeded-bug". The slug is
// lowercased, every run of characters outside [a-z0-9] becomes "-", the ends are trimmed of "-"
// and the slug is cut to 40 characters.
func Branch(typ, slug string) (string, error) {
	switch typ {
	case "feat", "fix", "chore", "docs", "test":
	default:
		return "", fmt.Errorf("branch type %q: type must be feat, fix, chore, docs or test", typ)
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
// <branch>-2, <branch>-3 and so on. A requested base names a branch on origin. Without one,
// the branch starts at origin/HEAD when that resolves, else HEAD. It returns the absolute path,
// the branch used and the base. No fetch, no network.
func Create(
	ctx context.Context, repoPath, root, repoName, branch, requestedBase string,
) (path, used string, base Base, err error) {
	start := startRef(ctx, repoPath)
	if requestedBase != "" {
		ref := "refs/remotes/origin/" + requestedBase
		_, validErr := git(ctx, repoPath, nil, "check-ref-format", "--branch", requestedBase)
		_, refErr := git(ctx, repoPath, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
		if validErr != nil || refErr != nil || requestedBase == branch || requestedBase == "HEAD" ||
			strings.HasPrefix(requestedBase, "origin/") || strings.HasPrefix(requestedBase, "refs/") {
			return "", "", Base{}, errors.New("base must be a plain branch on origin, e.g. test/x")
		}
		start = ref
	}
	commit, err := git(ctx, repoPath, nil, "rev-parse", "--short=7", start)
	if err != nil {
		return "", "", Base{}, fmt.Errorf("read base: %w", err)
	}
	baseName := start
	if requestedBase != "" {
		baseName = "origin/" + requestedBase
	}
	base = Base{Name: baseName + " " + commit}
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

// startRef is the ref start_work branches from and the read-only checkout sits at: origin/HEAD when
// it resolves, else HEAD.
func startRef(ctx context.Context, repoPath string) string {
	if _, err := git(ctx, repoPath, nil, "rev-parse", "--verify", "--quiet", "origin/HEAD^{commit}"); err == nil {
		return "origin/HEAD"
	}
	return "HEAD"
}

// defaultBranch is the local branch origin/HEAD points at.
func defaultBranch(ctx context.Context, repoPath string) (string, bool) {
	remote, err := git(ctx, repoPath, nil, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	return strings.TrimPrefix(remote, "origin/"), err == nil
}

func commits(count string) string {
	if count == "1" {
		return "1 commit"
	}
	return count + " commits"
}

// aheadNote names the local default branch (the one origin/HEAD points at) and how far it is ahead
// of origin/HEAD. It is empty when it is not ahead or cannot be read.
func aheadNote(ctx context.Context, repoPath string) string {
	local, ok := defaultBranch(ctx, repoPath)
	if !ok {
		return ""
	}
	count, err := git(ctx, repoPath, nil, "rev-list", "--count", "origin/HEAD..refs/heads/"+local)
	if err != nil || count == "0" {
		return ""
	}
	tip, err := git(ctx, repoPath, nil, "rev-parse", "--short=7", "refs/heads/"+local)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("local %s is %s ahead (%s); not included", local, commits(count), tip)
}

// BehindNote says how far the local default branch (the one origin/HEAD points at) is behind
// origin/HEAD. It is empty when it is not behind or cannot be read.
func BehindNote(ctx context.Context, repoPath string) string {
	local, ok := defaultBranch(ctx, repoPath)
	if !ok {
		return ""
	}
	count, err := git(ctx, repoPath, nil, "rev-list", "--count", "refs/heads/"+local+"..origin/HEAD")
	if err != nil || count == "0" {
		return ""
	}
	tip, err := git(ctx, repoPath, nil, "rev-parse", "--short=7", "origin/HEAD")
	if err != nil {
		return ""
	}
	return fmt.Sprintf("local %s is %s behind origin/HEAD (%s); the checkout is at origin/HEAD", local, commits(count), tip)
}

// Checkout makes dir a detached worktree of repoPath at the commit start_work would branch from
// and moves an existing one there. id is the checkout's stored git directory ID, empty when not
// stored yet. A checkout with local changes stays; the note then says how far behind it is.
func Checkout(ctx context.Context, repoPath, dir, id string) (note string, err error) {
	start := startRef(ctx, repoPath)
	commit, err := git(ctx, repoPath, nil, "rev-parse", "--verify", start+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("read base: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", fmt.Errorf("make checkout folder: %w", err)
		}
		if _, err := git(ctx, repoPath, nil, "worktree", "add", "--detach", dir, commit); err != nil {
			return "", fmt.Errorf("add checkout: %w", err)
		}
		return "", nil
	}
	if id != "" {
		common, err := GitCommonDir(ctx, repoPath)
		if err != nil {
			return "", err
		}
		ctx = WithWorktree(ctx, common, id)
	}
	head, err := git(ctx, dir, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read checkout HEAD: %w", err)
	}
	if head == commit {
		return "", nil
	}
	status, err := git(ctx, dir, nil, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("read checkout status: %w", err)
	}
	if status != "" {
		count, err := git(ctx, dir, nil, "rev-list", "--count", head+".."+commit)
		if err != nil || count == "0" {
			return "", nil
		}
		return fmt.Sprintf("the checkout has local changes, so it stays at %s, %s behind %s (%s)",
			head[:7], commits(count), start, commit[:7]), nil
	}
	if _, err := git(ctx, dir, nil, "checkout", "--detach", commit); err != nil {
		return "", fmt.Errorf("move checkout: %w", err)
	}
	return "", nil
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

// Prune drops git's records of worktrees of repoPath whose folder is gone.
func Prune(ctx context.Context, repoPath string) error {
	if _, err := git(context.WithoutCancel(ctx), repoPath, nil, "worktree", "prune"); err != nil {
		return fmt.Errorf("prune worktrees: %w", err)
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

// DefaultBranch is the repo's default branch: what origin/HEAD names, else the branch checked out.
// It is empty when neither resolves.
func DefaultBranch(ctx context.Context, repoPath string) string {
	if remote, err := git(ctx, repoPath, nil, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(remote, "origin/")
	}
	local, _ := git(ctx, repoPath, nil, "symbolic-ref", "--short", "HEAD")
	return local
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
	runsFilters := args[0] == "checkout" || args[0] == "status" || (args[0] == "worktree" && len(args) > 1 && args[1] == "add")
	if enabled, _ := ctx.Value(snapshotKey{}).(bool); enabled || runsFilters {
		pins, err := pinFilters(ctx, base, allEnv)
		if err != nil {
			return "", err
		}
		allEnv = append(allEnv, pins...)
	}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = allEnv
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(exit.Stderr))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// pinFilters returns command-scope config that sets every filter key defined in repo-local,
// included or per-worktree config to its global or system value, or to empty (false for
// required) when there is none. A filter a Bot planted does not run; the user's own does.
func pinFilters(ctx context.Context, base, env []string) ([]string, error) {
	list := exec.CommandContext(ctx, "git", append(base, "config", "--show-scope", "--null", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)...)
	list.Env = env
	out, err := list.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return nil, fmt.Errorf("list filters: %w", err)
	}
	global := map[string]string{}
	var repoKeys []string
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		key, val, _ := strings.Cut(fields[i+1], "\n")
		switch fields[i] {
		case "global", "system":
			global[key] = val
		case "local", "worktree":
			repoKeys = append(repoKeys, key)
		}
	}
	var pins []string
	for i, key := range repoKeys {
		val, ok := global[key]
		if !ok && strings.HasSuffix(key, ".required") {
			val = "false"
		}
		pins = append(pins, "GIT_CONFIG_KEY_"+strconv.Itoa(i)+"="+key, "GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+val)
	}
	return append(pins, "GIT_CONFIG_COUNT="+strconv.Itoa(len(repoKeys))), nil
}
