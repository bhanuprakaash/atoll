// Command claude is a fake Claude Code for the e2e tests. It answers --version and
// `auth status`, and in -p stream-json mode replays the recording in e2e/testdata/claude whose
// prompt the user message on stdin contains. Like the real CLI it calls the HTTP MCP servers of
// --mcp-config when a recording uses their tools, and asks --permission-prompt-tool before a call
// the recording marks with a "fake" ask line, unless --allowedTools allows it. Signed in means
// CLAUDE_CONFIG_DIR (default ~/.claude) holds .credentials.json. Every call is logged to
// <executable>.log.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"atoll/e2e/fakes/fake"
)

const _version = "2.1.306 (Claude Code)"

type (
	line struct {
		Type      string  `json:"type"`
		Subtype   string  `json:"subtype"`
		SessionID string  `json:"session_id"`
		IsReplay  bool    `json:"isReplay"`
		Message   message `json:"message"`
	}

	message struct {
		Content json.RawMessage `json:"content"`
	}

	recording struct {
		session string
		prompt  string
		resumed bool // only plays on --resume of its session
		lines   []string
	}

	authStatus struct {
		LoggedIn        bool   `json:"loggedIn"`
		AuthMethod      string `json:"authMethod"`
		ConfigDirectory string `json:"configDirectory"`
	}
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fake claude:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	log, err := fake.Start()
	if err != nil {
		return err
	}
	switch {
	case slices.Contains(args, "--version"):
		fmt.Println(_version)
		return nil
	case len(args) >= 2 && args[0] == "auth" && args[1] == "status":
		return status()
	case slices.Contains(args, "-p") || slices.Contains(args, "--print"):
		return stream(log, args)
	}
	return fmt.Errorf("unsupported arguments %q", args)
}

func status() error {
	dir := fake.ConfigDir("CLAUDE_CONFIG_DIR", ".claude")
	_, err := os.Stat(filepath.Join(dir, ".credentials.json"))
	st := authStatus{LoggedIn: err == nil, AuthMethod: "none", ConfigDirectory: dir}
	if st.LoggedIn {
		st.AuthMethod = "claude.ai"
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	if !st.LoggedIn {
		os.Exit(1)
	}
	return nil
}

func stream(log *fake.Log, args []string) error {
	if flag(args, "--input-format") != "stream-json" || flag(args, "--output-format") != "stream-json" {
		return errors.New("want --input-format stream-json --output-format stream-json")
	}
	if slices.Contains(args, "--system-prompt") || slices.Contains(args, "--system-prompt-file") {
		return errors.New("reef must append, not replace the provider prompt")
	}
	if err := log.Prompt("system", flag(args, "--append-system-prompt")); err != nil {
		return err
	}
	recs, err := load()
	if err != nil {
		return err
	}
	p, err := newPlayer(args)
	if err != nil {
		return err
	}
	resume := flag(args, "--resume")
	in := fake.Lines()
	for in.Scan() {
		if err := log.Stdin(in.Text()); err != nil {
			return err
		}
		var l line
		if json.Unmarshal(in.Bytes(), &l) != nil || l.Type != "user" {
			continue
		}
		prompt, err := text(l.Message.Content)
		if err != nil {
			return err
		}
		if err := log.Prompt("user", prompt); err != nil {
			return err
		}
		p.userMessage = prompt
		rec, ok := find(recs, prompt, resume)
		if !ok {
			return fmt.Errorf("no recording for prompt %q (resume %q)", prompt, resume)
		}
		if err := p.play(rec.lines); err != nil {
			return err
		}
	}
	return in.Err()
}

// text is the text of a user message: a string, or the text blocks of a content array.
func text(content json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s, nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return "", fmt.Errorf("user message content is neither a string nor blocks: %s", content)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// find returns the recording whose prompt the message contains, the longest if several do.
// With a resume id it must belong to that session; without one it must not be marked resumed.
func find(recs []recording, prompt, resume string) (recording, bool) {
	var best recording
	for _, r := range recs {
		if r.prompt != "" && strings.Contains(prompt, r.prompt) && (resume == "" && !r.resumed || r.session == resume) &&
			len(r.prompt) > len(best.prompt) {
			best = r
		}
	}
	return best, best.prompt != ""
}

func load() ([]recording, error) {
	all, err := fake.Recordings("claude")
	if err != nil {
		return nil, err
	}
	recs := make([]recording, 0, len(all))
	for _, lines := range all {
		r := recording{lines: lines}
		for _, s := range lines {
			if fake.Directive([]byte(s)) == "resumed" {
				r.resumed = true
				continue
			}
			var l line
			if err := json.Unmarshal([]byte(s), &l); err != nil {
				return nil, err
			}
			if l.Type == "system" && l.Subtype == "init" {
				r.session = l.SessionID
			}
			if l.Type == "user" && l.IsReplay {
				_ = json.Unmarshal(l.Message.Content, &r.prompt)
			}
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// flag returns the value after name in args, or "".
func flag(args []string, name string) string {
	i := slices.Index(args, name)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}
