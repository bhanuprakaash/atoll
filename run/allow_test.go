package run

import (
	"encoding/json"
	"testing"
)

func TestCanAllowInThread(t *testing.T) {
	roots := []string{"/tmp", "/var/tmp", "", "/work/wt-1"}
	tests := []struct {
		give string
		want bool
	}{
		{"go clean -cache", true},
		{"go test -coverprofile=c.out ./...", true},
		{"rm -rf /tmp/codex-gocache-161", true},
		{"rm -rf /tmp/a /var/tmp/b", true},
		{"rm -rf /work/wt-1/build", true},
		{"rm -Rf /work/wt-1/build/", true},
		{"rm --recursive --force /tmp/a", true},
		{"rm -rf /tmp", false},
		{"rm -rf /work/wt-1", false},
		{"rm -rf /work/wt-2/build", false},
		{"rm -rf /var/lib/x", false},
		{"rm -fr /tmp/a /var/lib/x", false},
		{"rm -rf /tmp/../etc", false},
		{"rm -rf ~/.cache/x", false},
		{"rm -rf build", false},
		{"rm -rf /tmp/*", false},
		{"rm -rf /tmp/$X", false},
		{"rm -rf /tmp/a; ls /etc", false},
		{"rm -rf /tmp/a && cd /etc", false},
		{"rm -rf /tmp/a;rm -rf /etc", false},
		{`rm -rf "/tmp/a"`, false},
		{"rm --recursive /var/lib/x", true},
		{"rm --recursive --force /var/lib/x", false},
		{"git push origin x", false},
		{"GIT PUSH", false},
		{"gh repo delete o/r", false},
		{"rm -rf /tmp/delete-me", false},
		{"gh pr view 12", true},
		{"gh pr list --state open", true},
		{"gh run watch 1", true},
		{"gh status", true},
		{"gh api repos/o/r/pulls/12", true},
		{"gh api -X GET repos/o/r", true},
		{"gh api --method=get repos/o/r", true},
		{"gh api -X POST repos/o/r/issues", false},
		{"gh api -XPOST repos/o/r/issues", false},
		{"gh api --method PATCH repos/o/r", false},
		{"gh api repos/o/r -f title=x", false},
		{"gh api repos/o/r --input body.json", false},
		{"gh pr create --title x", false},
		{"gh pr merge 12", false},
		{"gh issue comment 3", false},
		{"gh workflow run ci.yml", false},
		{"gh pr", false},
		{"gh", false},
		{"gh pr view 1 && gh pr merge 1", false},
		{"cd /tmp/x && gh pr edit 3", false},
		{"echo $(gh pr merge 1)", false},
		{`bash -c 'gh pr merge 1'`, false},
		{"/usr/bin/gh issue close 3", false},
	}
	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			if got := canAllowInThread(tt.give, roots); got != tt.want {
				t.Errorf("canAllowInThread(%q) = %v, want %v", tt.give, got, tt.want)
			}
		})
	}
}

func TestCommandText(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
		ok    bool
	}{
		{"claude", "Bash", `{"command":"  go test ./... \n"}`, "go test ./...", true},
		{"codex wrapper", "commandExecution", `{"command":"/bin/bash -lc \"rm -rf /tmp/a\""}`, "rm -rf /tmp/a", true},
		{"codex single quotes", "commandExecution", `{"command":"/bin/zsh -c 'echo '\\''x'\\'' $HOME'"}`, "echo 'x' $HOME", true},
		{"codex newline", "commandExecution", `{"command":"/bin/bash -lc \"  rm -rf /tmp/a \n\""}`, "rm -rf /tmp/a", true},
		{"codex plain", "commandExecution", `{"command":"ls"}`, "ls", true},
		{"edit", "Edit", `{"command":"ls"}`, "", false},
		{"mcp", "mcp__github__merge", `{"command":"ls"}`, "", false},
		{"empty", "Bash", `{"command":" "}`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := commandText(tt.tool, json.RawMessage(tt.input))
			if got != tt.want || ok != tt.ok {
				t.Errorf("commandText = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
