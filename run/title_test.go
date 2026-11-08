package run

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"atoll/provider"
)

func TestWriteTitle(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GH_CONFIG_DIR", "/personal/gh")
			t.Setenv("GLAB_CONFIG_DIR", "/personal/glab")
			e := newEnv(t, &fakeSession{script: []provider.Event{
				{Kind: provider.KindText, Text: "A title"}, {Kind: provider.KindTurnEnd},
			}})
			title, err := e.m.writeTitle(t.Context(), name, "message", "reply")
			if err != nil || title != "A title" {
				t.Fatalf("title = %q, %v; want A title", title, err)
			}
			cfg := e.sess.cfgs[0]
			if !cfg.NoTools {
				t.Error("title session has tools enabled")
			}
			for _, want := range []string{
				"GH_CONFIG_DIR=" + filepath.Join(e.m.cfg.DataDir, "empty", "gh"),
				"GLAB_CONFIG_DIR=" + filepath.Join(e.m.cfg.DataDir, "empty", "glab"),
				"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
				"GIT_SSH_COMMAND=ssh -F /dev/null -o IdentitiesOnly=yes -o IdentityFile=/dev/null -o IdentityAgent=none",
			} {
				if !slices.Contains(cfg.Env, want) {
					t.Errorf("title env lacks %q", want)
				}
			}
			for _, path := range []string{"empty/gh/hosts.yml", "empty/glab/config.yml"} {
				data, err := os.ReadFile(filepath.Join(e.m.cfg.DataDir, path))
				if err != nil || string(data) != "" {
					t.Errorf("empty config %s = %q, %v", path, data, err)
				}
			}
		})
	}
}

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{name: "plain", give: "Fix login redirect loop", want: "Fix login redirect loop"},
		{name: "first letter up", give: "fix login redirect loop", want: "Fix login redirect loop"},
		{name: "trailing punctuation", give: "Fix the login redirect.", want: "Fix the login redirect"},
		{name: "quotes", give: `"Fix the login redirect"`, want: "Fix the login redirect"},
		{name: "six words", give: "One two three four five six seven", want: "One two three four five six"},
		{name: "first line only", give: "Fix login\n\nThis title says", want: "Fix login"},
		{name: "markdown heading", give: "## Fix login", want: "Fix login"},
		{name: "empty", give: " \n", want: ""},
		{name: "only punctuation", give: ".", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanTitle(tt.give); got != tt.want {
				t.Errorf("cleanTitle(%q) = %q, want %q", tt.give, got, tt.want)
			}
		})
	}
}

func TestHelperBrief(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{name: "preface", give: "Poteto delegated this to you; your replies go to Poteto.\n\nRun the tests", want: "Run the tests"},
		{
			name: "preface and branch",
			give: "Poteto delegated this to you; your replies go to Poteto.\n\nWork on branch feat/x--tester and commit there.\n\nRun the tests\n\nThen report",
			want: "Run the tests\n\nThen report",
		},
		{name: "no preface", give: "Run the tests\n\nThen report", want: "Run the tests\n\nThen report"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := helperBrief(tt.give); got != tt.want {
				t.Errorf("helperBrief(%q) = %q, want %q", tt.give, got, tt.want)
			}
		})
	}
}
