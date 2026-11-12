package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"atoll/e2e/fakes/fake"
)

// fsCall is one call of the fs-read scenario: the prompt "Read files. " followed by a JSON array.
// Tool is Bash, Read, Glob, Grep or Write. A Bash call reads Path's first line with Via "head"
// (the default) or "python".
type fsCall struct {
	Tool string `json:"tool"`
	Path string `json:"path"`
	Via  string `json:"via"`
}

// fsSettings is what the --settings JSON gives the fs-read scenario: permission deny rules and the
// sandbox, merged by top-level key like claude does.
type fsSettings struct {
	Permissions struct {
		Deny []string `json:"deny"`
	} `json:"permissions"`
	Sandbox struct {
		Enabled    bool `json:"enabled"`
		Filesystem struct {
			DenyRead []string `json:"denyRead"`
		} `json:"filesystem"`
	} `json:"sandbox"`
}

func loadFSSettings(args []string) (fsSettings, error) {
	var out fsSettings
	for _, v := range values(args, "--settings") {
		src := []byte(v)
		if !strings.HasPrefix(strings.TrimSpace(v), "{") {
			b, err := os.ReadFile(v)
			if err != nil {
				return out, err
			}
			src = b
		}
		if err := json.Unmarshal(src, &out); err != nil {
			return out, fmt.Errorf("--settings %s: %w", v, err)
		}
	}
	return out, nil
}

// fsRead plays the calls the way Claude Code 2.1.322 treated them in the spike of #169: a
// permission deny rule Read(//dir/**) refuses Read, Glob and Grep, and Edit(//dir/**) refuses
// Write, each on the path as given and with symlinks resolved. It also refuses Bash `head` and
// `cat` of a path a Read rule names, but not other programs (python reads the file). Only the
// sandbox hides a path from every Bash command: a path under filesystem.denyRead has no file.
// The reply has one line per call.
func (p *player) fsRead(args []string) (string, error) {
	_, payload, ok := strings.Cut(p.userMessage, "Read files. ")
	if !ok {
		return "", fmt.Errorf("fs-read payload absent")
	}
	var calls []fsCall
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &calls); err != nil {
		return "", fmt.Errorf("fs-read payload: %w", err)
	}
	set, err := loadFSSettings(args)
	if err != nil {
		return "", err
	}
	tools := strings.Split(flag(args, "--tools"), ",")
	var lines []string
	for i, c := range calls {
		label, input := c.Tool, map[string]string{"file_path": c.Path}
		switch c.Tool {
		case "Bash":
			if c.Via == "" {
				c.Via = "head"
			}
			label = "Bash:" + c.Via
			cmd := "head -1 " + c.Path
			if c.Via == "python" {
				cmd = `python3 -c "print(open('` + c.Path + `').readline())"`
			}
			input = map[string]string{"command": cmd}
		case "Glob":
			input = map[string]string{"pattern": "*", "path": c.Path}
		case "Grep":
			input = map[string]string{"pattern": ".", "path": c.Path}
		case "Write":
			input["content"] = "reef"
		}
		raw, _ := json.Marshal(input)
		use := toolUse{Type: "tool_use", ID: fmt.Sprintf("toolu_fs_read_%d", i+1), Name: c.Tool, Input: raw}
		p.uses[use.ID] = use
		emit := func(l map[string]any) {
			l["parent_tool_use_id"], l["session_id"] = nil, p.session
			b, _ := json.Marshal(l)
			fmt.Println(string(b))
		}
		emit(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "type": "message", "content": []toolUse{use}}})
		time.Sleep(fake.Pace)
		text, isErr := "", false
		switch {
		case !slices.Contains(tools, c.Tool):
			text, isErr = "No such tool available: "+c.Tool, true
		default:
			text, isErr = set.serve(c)
		}
		result := map[string]any{"type": "tool_result", "tool_use_id": use.ID, "content": []map[string]string{{"type": "text", "text": text}}}
		if isErr {
			result["is_error"] = true
		}
		emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{result}}})
		outcome := text
		if isErr {
			outcome = "refused: " + text
		}
		lines = append(lines, label+" "+c.Path+": "+outcome)
	}
	return strings.Join(lines, "\n"), nil
}

// serve runs one call and returns what the tool answered.
func (s fsSettings) serve(c fsCall) (text string, isErr bool) {
	forms := []string{c.Path}
	if r, err := filepath.EvalSymlinks(c.Path); err == nil && r != c.Path {
		forms = append(forms, r)
	}
	switch c.Tool {
	case "Read", "Glob", "Grep":
		if s.denied("Read", forms) {
			return "denied by your permission settings", true
		}
	case "Write":
		if s.denied("Edit", forms) {
			return "denied by your permission settings", true
		}
		if err := os.WriteFile(c.Path, []byte("reef"), 0o600); err != nil {
			return err.Error(), true
		}
		return "wrote", false
	case "Bash":
		if s.Sandbox.Enabled && s.hidden(forms) {
			return "No such file or directory", true
		}
		if c.Via != "python" && s.denied("Read", forms) {
			return "Permission to use Bash has been denied", true
		}
	}
	if c.Tool == "Glob" || c.Tool == "Grep" {
		entries, err := os.ReadDir(c.Path)
		if err != nil {
			return err.Error(), true
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return strings.Join(names, " "), false
	}
	b, err := os.ReadFile(c.Path)
	if err != nil {
		return err.Error(), true
	}
	first, _, _ := strings.Cut(string(b), "\n")
	return first, false
}

// denied reports whether a rule of kind (Read or Edit) names one of the forms of a path. A rule is
// kind(//absolute/dir/**); ? matches one character but never a slash.
func (s fsSettings) denied(kind string, forms []string) bool {
	for _, rule := range s.Permissions.Deny {
		inner, ok := strings.CutPrefix(rule, kind+"(")
		if !ok {
			continue
		}
		pattern, ok := strings.CutSuffix(strings.TrimSuffix(inner, ")"), "/**")
		if !ok || !strings.HasPrefix(pattern, "//") {
			continue
		}
		re := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern[1:]), `\?`, "[^/]") + "(/.*)?$"
		for _, f := range forms {
			if regexp.MustCompile(re).MatchString(f) {
				return true
			}
		}
	}
	return false
}

// hidden reports whether the sandbox hides one of the forms of a path.
func (s fsSettings) hidden(forms []string) bool {
	for _, f := range forms {
		for _, d := range s.Sandbox.Filesystem.DenyRead {
			if f == d || strings.HasPrefix(f, strings.TrimSuffix(d, "/")+"/") {
				return true
			}
		}
	}
	return false
}
