package run

import (
	"encoding/json"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"atoll/store"
)

var (
	_shellWrap = regexp.MustCompile(`(?s)^(?:\S*/)?(?:bash|sh|zsh) -l?c (.+)$`)
	_shortFlag = regexp.MustCompile(`^-[A-Za-z]+$`)
	_safePath  = regexp.MustCompile(`^/[A-Za-z0-9._+@%=,:/-]*$`)
	_separator = strings.NewReplacer(";", " ; ", "&", " ; ", "|", " ; ", "(", " ; ", ")", " ; ", "`", " ; ")
	_ghReads   = []string{"view", "list", "status", "diff", "checks", "search", "browse", "watch", "download"}
)

// commandText is the command of a command approval, Claude's Bash or Codex's commandExecution,
// trimmed and without Codex's shell wrapper. ok is false for any other tool.
func commandText(tool string, input json.RawMessage) (string, bool) {
	if tool != "Bash" && tool != "commandExecution" {
		return "", false
	}
	var in struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &in) != nil {
		return "", false
	}
	text := unwrapShell(in.Command)
	return text, text != ""
}

// unwrapShell returns the script of a `bash -lc '...'` wrapper without its quotes, else command.
// The result is trimmed.
func unwrapShell(command string) string {
	text := strings.TrimSpace(command)
	if m := _shellWrap.FindStringSubmatch(text); m != nil && len(m[1]) >= 2 {
		inner := m[1][1 : len(m[1])-1]
		switch m[1][0] {
		case '"':
			text = inner
		case '\'':
			text = strings.ReplaceAll(inner, `'\''`, "'")
		}
	}
	return strings.TrimSpace(text)
}

// allowRoots are the folders an rm -rf may delete inside: the tmp roots and the thread's worktree.
func allowRoots(thread store.Thread) []string {
	roots := []string{"/tmp", "/var/tmp", os.Getenv("TMPDIR")}
	if thread.Worktree != "" {
		roots = append(roots, thread.Worktree)
	}
	return roots
}

// canAllowInThread reports whether Allow in this thread is offered for command: never for push or
// delete, for an -rf that reaches anything but a plain absolute path inside roots, for a git
// command that discards work, or for a gh write.
func canAllowInThread(command string, roots []string) bool {
	lower := strings.ToLower(command)
	if strings.Contains(lower, "push") || strings.Contains(lower, "delete") {
		return false
	}
	words := strings.Fields(_separator.Replace(command))
	return (!recursiveForce(words) || allInside(words, roots)) && !ghWrite(words) && !gitDiscards(words)
}

// recursiveForce reports whether words hold both r and f: in one flag word, as --recursive and
// --force, or across the flag words of one rm.
func recursiveForce(words []string) bool {
	long := map[string]bool{}
	var rm, recursive, force bool
	for _, w := range words {
		lower := strings.ToLower(w)
		switch {
		case w == ";":
			rm, recursive, force = false, false, false
		case path.Base(w) == "rm":
			rm = true
		case w == "--recursive" || w == "--force":
			long[w] = true
			recursive, force = recursive || w == "--recursive", force || w == "--force"
		case _shortFlag.MatchString(w):
			if strings.Contains(lower, "r") && strings.Contains(lower, "f") {
				return true
			}
			recursive, force = recursive || strings.Contains(lower, "r"), force || strings.Contains(lower, "f")
		}
		if rm && recursive && force {
			return true
		}
	}
	return len(long) == 2
}

// allInside reports whether every word but the flags, rm and the separators is a plain absolute
// path strictly inside one of roots.
func allInside(words, roots []string) bool {
	for _, w := range words {
		if w == ";" || w == "rm" || strings.HasPrefix(w, "-") {
			continue
		}
		if !_safePath.MatchString(w) || slices.Contains(strings.Split(w, "/"), "..") ||
			!slices.ContainsFunc(roots, func(r string) bool { return inside(w, r) }) {
			return false
		}
	}
	return true
}

func inside(p, root string) bool {
	root = strings.TrimRight(root, "/")
	rest, ok := strings.CutPrefix(p, root+"/")
	return ok && root != "" && strings.Trim(rest, "/") != ""
}

// gitDiscards reports whether any git in words throws work away: clean with a force flag, reset
// --hard, checkout of paths, of `.` or with a force flag, or restore.
func gitDiscards(words []string) bool {
	for i, w := range words {
		if path.Base(strings.Trim(w, `"'`)) != "git" {
			continue
		}
		args := words[i+1:]
		if end := slices.Index(args, ";"); end >= 0 {
			args = args[:end]
		}
		for j := 0; j < len(args); j++ {
			a := strings.Trim(args[j], `"'`)
			if a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" {
				j++
				continue
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
			return gitDiscardsWith(a, args[j+1:])
		}
	}
	return false
}

func gitDiscardsWith(verb string, args []string) bool {
	has := func(match func(string) bool) bool {
		return slices.ContainsFunc(args, func(a string) bool { return match(strings.Trim(a, `"'`)) })
	}
	force := func(a string) bool {
		return a == "--force" || _shortFlag.MatchString(a) && strings.Contains(a, "f")
	}
	switch verb {
	case "restore":
		return true
	case "clean":
		return has(force)
	case "reset":
		return has(func(a string) bool { return a == "--hard" })
	case "checkout":
		return has(func(a string) bool { return a == "--" || a == "." || force(a) })
	}
	return false
}

// ghWrite reports whether any gh in words is not a read.
func ghWrite(words []string) bool {
	for i, w := range words {
		if path.Base(strings.Trim(w, `"'`)) != "gh" {
			continue
		}
		args := words[i+1:]
		if end := slices.Index(args, ";"); end >= 0 {
			args = args[:end]
		}
		if !ghRead(args) {
			return true
		}
	}
	return false
}

func ghRead(args []string) bool {
	var verbs []string
	for _, a := range args {
		if a = strings.Trim(a, `"'`); !strings.HasPrefix(a, "-") {
			verbs = append(verbs, a)
		}
	}
	switch {
	case len(verbs) == 0:
		return false
	case verbs[0] == "api":
		return !apiWrite(args)
	}
	return slices.Contains(_ghReads, verbs[0]) || len(verbs) > 1 && slices.Contains(_ghReads, verbs[1])
}

// apiWrite reports whether gh api's args send a non-GET method, a field or an input.
func apiWrite(args []string) bool {
	for i, a := range args {
		a = strings.Trim(a, `"'`)
		switch {
		case a == "-X" || a == "--method":
			if i+1 == len(args) || !strings.EqualFold(strings.Trim(args[i+1], `"'`), "GET") {
				return true
			}
		case strings.HasPrefix(a, "--method="), strings.HasPrefix(a, "-X"):
			if !strings.EqualFold(a[strings.IndexAny(a, "=X")+1:], "GET") {
				return true
			}
		case strings.HasPrefix(a, "--field"), strings.HasPrefix(a, "--raw-field"),
			strings.HasPrefix(a, "--input"), strings.HasPrefix(a, "-f"), strings.HasPrefix(a, "-F"):
			return true
		}
	}
	return false
}
