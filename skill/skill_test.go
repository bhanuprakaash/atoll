package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"atoll/store"
)

func TestReadSymlinks(t *testing.T) {
	tests := []struct {
		name    string
		link    string
		target  string
		wantErr bool
	}{
		{name: "outside file", link: "reference.md", target: "outside.md"},
		{name: "outside directory", link: "references", target: "."},
		{name: "dangling link", link: "missing.md", target: "missing.md"},
		{name: "skill file", link: "SKILL.md", target: "outside.md", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "skill")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "outside.md"), []byte("private"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.link != "SKILL.md" {
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("local skill"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(filepath.Join(root, tt.target), filepath.Join(dir, tt.link)); err != nil {
				t.Fatal(err)
			}
			got, err := Read(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Read accepted a symlink as SKILL.md")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Skill.Content != "local skill" || len(got.Skill.Files) != 0 {
				t.Errorf("Read imported linked content: %+v", got.Skill)
			}
			want := []Skipped{{Path: tt.link, Reason: SkippedOther}}
			if !reflect.DeepEqual(got.Skipped, want) {
				t.Errorf("Skipped = %v, want %v", got.Skipped, want)
			}
		})
	}
}

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

func TestCheck(t *testing.T) {
	tests := []struct {
		name string
		give []string
		ok   bool
	}{
		{"nested", []string{"scripts/check", "playbooks/ship.md"}, true},
		{"parent", []string{"../x"}, false},
		{"parent inside", []string{"scripts/../../x"}, false},
		{"absolute", []string{"/etc/passwd"}, false},
		{"dot", []string{"./x"}, false},
		{"empty element", []string{"a//b"}, false},
		{"SKILL.md", []string{"SKILL.md"}, false},
		{"twice", []string{"a", "a"}, false},
		{"file and folder", []string{"a", "a/b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := make([]store.SkillFile, len(tt.give))
			for i, p := range tt.give {
				files[i] = store.SkillFile{Path: p, Content: "x"}
			}
			if err := Check(files); (err == nil) != tt.ok {
				t.Fatalf("Check(%q) = %v, want ok %t", tt.give, err, tt.ok)
			}
		})
	}
	for _, content := range []string{"\xff\xfe", "a\x00b"} {
		if Check([]store.SkillFile{{Path: "a", Content: content}}) == nil {
			t.Errorf("Check(content %q) = nil, want an error", content)
		}
	}
}

func TestReadHomeRefs(t *testing.T) {
	tests := []struct {
		name        string
		give        string
		wantContent string
		wantRefs    []string
	}{
		{"own folder ends the sentence", "See ~/.claude/skills/foo.\n", "See {{ATOLL_SKILL_DIR}}.\n", nil},
		{"own folder then more text", "See ~/.claude/skills/foo. Next.\n", "See {{ATOLL_SKILL_DIR}}. Next.\n", nil},
		{"own folder in parentheses", "(~/.claude/skills/foo.)\n", "({{ATOLL_SKILL_DIR}}.)\n", nil},
		{"dotted name is another folder", "~/.claude/skills/foo.bar/x\n", "~/.claude/skills/foo.bar/x\n",
			[]string{"~/.claude/skills/foo.bar/x"}},
		{"tilde word is prose", "It takes ~half the time.\n", "It takes ~half the time.\n", nil},
		{"existing user", "Run cd ~root now.\n", "Run cd ~root now.\n", []string{"~root"}},
		{"existing user with path", "Read ~root/.ssh/id now.\n", "Read ~root/.ssh/id now.\n",
			[]string{"~root/.ssh/id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), ".claude", "skills", "foo")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(tt.give), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Read(dir)
			if err != nil {
				t.Fatal(err)
			}
			var refs []string
			for _, r := range got.Refs {
				refs = append(refs, r.Ref)
			}
			if got.Skill.Content != tt.wantContent || !reflect.DeepEqual(refs, tt.wantRefs) {
				t.Errorf("Read = %q, refs %q, want %q, refs %q", got.Skill.Content, refs, tt.wantContent, tt.wantRefs)
			}
		})
	}
}
