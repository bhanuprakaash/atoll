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

// TitleFor is the title the fakes write for a title prompt.
func TitleFor(prompt string) (string, bool) {
	return Title, strings.Contains(prompt, TitleTrigger)
}
