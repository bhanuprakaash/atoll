package skill

import "testing"

func TestStrip(t *testing.T) {
	const greeting = "---\nname: reef-greeting\ndescription: Give the Reef greeting.\n" +
		"allowed-tools: Read\ncontext: fork\n---\nHello from the lagoon.\n"
	const list = "---\nname: a\nallowed-tools:\n  - Read\n  - Bash\ncontext:\n  fork: true\n" +
		"description: d\n---\nallowed-tools: kept in body\n"
	const bare = "---\nname: a\nallowed-tools:\n- Read\ndescription: d\n---\nbody\n"
	const hidden = "---\nname: a\ndisable-model-invocation: true\nallowed-tools: Read\ndescription: d\n---\n" +
		"disable-model-invocation: kept in body\n"
	const none = "Just text.\nallowed-tools: Read\n"

	tests := []struct {
		name     string
		provider string
		give     string
		want     string
	}{
		{"claude unchanged", "claude", greeting, greeting},
		{"codex drops both keys", "codex", greeting,
			"---\nname: reef-greeting\ndescription: Give the Reef greeting.\n---\nHello from the lagoon.\n"},
		{"codex drops list values", "codex", list,
			"---\nname: a\ndescription: d\n---\nallowed-tools: kept in body\n"},
		{"codex drops unindented list", "codex", bare, "---\nname: a\ndescription: d\n---\nbody\n"},
		{"claude drops disable-model-invocation", "claude", hidden,
			"---\nname: a\nallowed-tools: Read\ndescription: d\n---\ndisable-model-invocation: kept in body\n"},
		{"codex drops disable-model-invocation too", "codex", hidden,
			"---\nname: a\ndescription: d\n---\ndisable-model-invocation: kept in body\n"},
		{"claude without frontmatter", "claude", none, none},
		{"codex without frontmatter", "codex", none, none},
		{"codex CRLF", "codex", "---\r\nname: a\r\ncontext: fork\r\n---\r\nbody\r\n",
			"---\r\nname: a\r\n---\r\nbody\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Strip(tt.give, tt.provider); got != tt.want {
				t.Errorf("Strip = %q, want %q", got, tt.want)
			}
		})
	}
}
