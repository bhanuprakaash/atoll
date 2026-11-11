package fake

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Commands reads the command-use prompt: "Run commands. " followed by a JSON array of command
// strings. The fake runs each as the provider's shell tool, asking for approval first, and replies
// with one CommandLine per command.
func Commands(prompt string) ([]string, error) {
	_, payload, ok := strings.Cut(prompt, "Run commands. ")
	if !ok {
		return nil, fmt.Errorf("command-use payload absent")
	}
	var cmds []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &cmds); err != nil {
		return nil, fmt.Errorf("command-use payload: %w", err)
	}
	return cmds, nil
}

// CommandLine is what one command adds to the reply: "<command>: ran", or "<command>: denied"
// when the user said no. The command is trimmed.
func CommandLine(cmd, text string) string {
	return strings.TrimSpace(cmd) + ": " + text
}

// DeniedText is what a command the user refused adds to its line: "denied", and when the user wrote
// a reason, ", told: <reason>". The reason is what the provider was given: Claude's deny message,
// Codex's steer text. Reef's stock message for a plain Deny is no reason.
func DeniedText(reason string) string {
	if reason == "" || reason == "The user rejected this step." {
		return "denied"
	}
	return "denied, told: " + reason
}

// ShellWrap is how Codex reports a command: the shell it ran it in, with the text quoted.
func ShellWrap(cmd string) string {
	if strings.ContainsAny(cmd, "\"$`\\") {
		return "/bin/bash -lc '" + strings.ReplaceAll(cmd, "'", `'\''`) + "'"
	}
	return `/bin/bash -lc "` + cmd + `"`
}
