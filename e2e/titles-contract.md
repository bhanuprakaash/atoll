# Titles test contract

Extends [chat-polish-contract.md](chat-polish-contract.md) and
[delegation-contract.md](delegation-contract.md) for issue #93. Go tests drive the real binary
over HTTP with the scripted fake providers.

## Generated titles

After a thread's first reply, Reef asks the Bot's provider for a title in a session of its own:
Claude Code with `--model haiku`, Codex on `gpt-6-luna`. Its system prompt starts
`You name chat threads.`; it never resumes the thread's session, has no MCP config, is no run (the
thread keeps its one run and two messages) and sends nothing on the thread's event stream. The
reply becomes the title: its first line, at most six words, first letter upper case, no trailing
punctuation.

Until the title lands, and when the title session fails, the title stays the first six words.
The fakes answer a title session only when its prompt holds `fake.TitleTrigger` (with
`fake.Title`); every other title session fails. A title set by `PATCH` before the first reply is
never replaced and no title session runs for it.

## Helper threads

The brief in a helper thread is stored with `from_bot_id` set to the boss Bot. The helper
thread's title is the first six words of the brief, without the `{Boss} delegated this to you`
preface, and the title session's prompt does not hold the preface.

## Call logs

`machine.calls` leaves title sessions out; `machine.titles` returns only them.

## Label prefixes and cut titles

Rules are in design.md, Chat (thread titles). `TestTitles_AnswerRules` gives the title session raw
answers (`fake.TitleAnswer`) for both providers: a leading `Title:`, `Conversation title:`,
`Thread title:`, `Chat title:` or `Status line:` (any case, optionally in `**`) is dropped before
the six-word cut; another colon stays; a cut answer loses trailing stop words and an uncut one
keeps its last word; an answer with nothing left leaves the first-words title.
`TestTitles_FirstWordsRules` covers the first-words title (the title session fails): cut on stop
words, labels kept. `TestTitles_LaunchCleansStoredTitles` writes titles into the database, restarts
the server and expects labels dropped and six-word titles cut on a stop word trimmed.
