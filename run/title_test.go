package run

import "testing"

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
