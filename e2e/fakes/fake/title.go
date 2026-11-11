package fake

import "strings"

// TitleSystem starts the system prompt of Reef's title sessions.
const TitleSystem = "You name chat threads."

// TitleTrigger is the text a title prompt must hold for the fakes to write Title; every other
// title session fails, so its thread keeps the first-words title.
const (
	TitleTrigger = "the login redirect loops on the settings page"
	Title        = "login redirect loop on settings."
)

// A message holding "[[answer: X]]" makes the fakes' title session answer X as it stands, so a
// test can give a title session any raw answer.
const (
	answerOpen  = "[[answer:"
	answerClose = "]]"
)

// TitleAnswer is the marker that makes the fakes answer raw.
func TitleAnswer(raw string) string { return answerOpen + " " + raw + answerClose }

// TitleFor is the title the fakes write for a title prompt.
func TitleFor(prompt string) (string, bool) {
	if _, rest, ok := strings.Cut(prompt, answerOpen); ok {
		if raw, _, ok := strings.Cut(rest, answerClose); ok {
			return strings.TrimSpace(raw), true
		}
	}
	return Title, strings.Contains(prompt, TitleTrigger)
}
