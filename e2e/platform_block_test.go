package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	_platformBlockMaxBytes = 2000
	_guideHeading          = "Reef guide"
	_askHeading            = "Ask first"
	_voiceHeading          = "Voice"
)

// platformBlock returns the system prompt from the line `Managing Reef` up to the persona.
func platformBlock(t *testing.T, system string) string {
	t.Helper()
	start := strings.Index(system, "\nManaging Reef\n")
	if start < 0 {
		t.Fatalf("no Managing Reef line:\n%s", system)
	}
	block := system[start+1:]
	if end := strings.Index(block, "\n\nPersona:\n"); end >= 0 {
		block = block[:end]
	}
	return strings.TrimRight(block, "\n")
}

// section returns the lines after the exact line heading, up to the next blank line.
func section(t *testing.T, block, heading string) []string {
	t.Helper()
	lines := strings.Split(block, "\n")
	i := slices.Index(lines, heading)
	if i < 0 {
		t.Fatalf("platform block has no %q line:\n%s", heading, block)
	}
	var body []string
	for _, line := range lines[i+1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		body = append(body, line)
	}
	if len(body) == 0 {
		t.Fatalf("%q has no lines:\n%s", heading, block)
	}
	return body
}

// chiefVoice sends "hey" to Chief and returns the system prompt and its voice lines.
func chiefVoice(t *testing.T, s *server, p string) (string, []string) {
	t.Helper()
	system := heySystem(t, s, p, chief(t, s).bot)
	return system, section(t, platformBlock(t, system), _voiceHeading)
}

func TestPrompt_ChiefPlatformBlock(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			system, voice := chiefVoice(t, s, p)
			assertParts(t, system, "core", "platform")
			block := platformBlock(t, system)
			if n := len(block); n > _platformBlockMaxBytes {
				t.Fatalf("platform block is %d bytes, want at most %d:\n%s", n, _platformBlockMaxBytes, block)
			}
			guide := strings.Join(section(t, block, _guideHeading), "\n")
			if !strings.Contains(guide, "MCP") || !strings.Contains(strings.ToLower(guide), "connection") {
				t.Fatalf("Reef guide names neither connections nor MCP:\n%s", guide)
			}
			section(t, block, _askHeading)
			if len(voice) > 3 {
				t.Fatalf("voice is %d lines, want at most 3:\n%s", len(voice), strings.Join(voice, "\n"))
			}
			persona := chief(t, s).Persona
			_, promptPersona, _ := strings.Cut(system, "\n\nPersona:\n")
			for _, line := range voice {
				if strings.Contains(persona, line) || strings.Contains(promptPersona, line) {
					t.Fatalf("Chief's persona carries the voice line %q:\n%s", line, persona)
				}
			}
		})
	}
}

func TestPrompt_ChiefSectionsSay(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			block := platformBlock(t, heySystem(t, s, p, chief(t, s).bot))
			for _, c := range []struct {
				heading, name string
				lower         bool
				has, lacks    []string
				blockLacks    []string
			}{
				{heading: _importHeading, name: "import_agent", has: []string{"import_agent"}, blockLacks: []string{"read it,", "file tool", "Read tool"}},
				{heading: _importHeading, name: "import_skill and persona", has: []string{"import_skill", "persona"}},
				{heading: _importHeading, name: "refused, not flagged", has: []string{"refused"}, lacks: []string{"flagged"}},
				{heading: _askHeading, name: "the full proposal", lower: true, has: []string{"proposal", "model", "skills", "repo", "connections"}},
			} {
				text := strings.Join(section(t, block, c.heading), "\n")
				if c.lower {
					text = strings.ToLower(text)
				}
				for _, want := range c.has {
					if !strings.Contains(text, want) {
						t.Errorf("%q does not name %q (%s):\n%s", c.heading, want, c.name, text)
					}
				}
				for _, bad := range c.lacks {
					if strings.Contains(text, bad) {
						t.Errorf("%q names %q (%s):\n%s", c.heading, bad, c.name, text)
					}
				}
				for _, bad := range c.blockLacks {
					if strings.Contains(block, bad) {
						t.Errorf("platform block still tells Chief to read files itself (%q):\n%s", bad, block)
					}
				}
			}
		})
	}
}

func TestPrompt_NoSwitchNoPlatformTextNoVoice(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			_, voice := chiefVoice(t, s, p)
			for _, access := range []any{"all", []string{}} {
				var b bot
				s.do(t, "POST", "/api/bots", map[string]any{"name": "Talker", "persona": "Answers questions.", "provider": p, "repo_access": access}, &b, http.StatusCreated)
				system := heySystem(t, s, p, b)
				lines := strings.Split(system, "\n")
				for _, heading := range []string{"Managing Reef", _guideHeading, _askHeading, _voiceHeading} {
					if slices.Contains(lines, heading) {
						t.Fatalf("Bot without the switch (repo_access %v) has the line %q:\n%s", access, heading, system)
					}
				}
				for _, line := range voice {
					if strings.Contains(system, line) {
						t.Fatalf("Bot without the switch (repo_access %v) has Chief's voice line %q:\n%s", access, line, system)
					}
				}
			}
		})
	}
}
