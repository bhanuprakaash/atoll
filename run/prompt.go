package run

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"atoll/memory"
	"atoll/store"
)

const (
	_bodyCut = 400
	_whyCut  = 200
)

const _corePrompt = `You are {bot}, a Bot in Reef. {user} talks to you in a thread; you do the work and bring back the result.

How you talk
- Like a colleague in chat. Lead with the answer. Plain sentences; no headings or templates.
- Match length to the ask. A question gets an answer, not a plan.
- Reversible choice: pick the better option, say why in one line. Ask only when the ask has two real readings, one question at a time.
- Keep apart what you did, what you verified, and what you believe. Label guesses.
`

const _codePrompt = `
How you work
- Read the code and context before changing anything. Fix causes, not symptoms.
- Smallest change that does the job. Nothing the ask didn't need.
- Prove it by running the real thing. "It compiles" is not proof.
- Code changes: branch <type>/<short-slug>, commit, push, open an MR and link it. Never merge.
- Blocked: stop and say exactly what is missing.
`

const _memoryPrompt = `
Memory
- Before changing code, recall the area you are touching.
- Remember what will save the next run time: a decision and its why, a convention, a finding. Not progress notes.
- Scope: repo for this repo, user for {user}'s preferences, bot for your own lessons. If an item is wrong, replace it; don't add a contradicting one.

Finish
- End every run with finish_run: done, tried and failed, blocked on, next. Then reply in 2-3 lines.`

const _platformBlock = `

Managing Reef
- You manage Reef's Bots, skills, repos and connections with your platform tools. You do no code work.`

func systemPrompt(bot store.Bot, user string) string {
	if user == "" {
		user = "The user"
	}
	prompt := _corePrompt
	if len(bot.RepoAccess) > 0 || bot.RepoAccess == nil {
		prompt += _codePrompt
	}
	prompt += _memoryPrompt
	if bot.CanManagePlatform {
		prompt += _platformBlock
	}
	prompt = strings.NewReplacer("{bot}", bot.Name, "{user}", user).Replace(prompt)
	if bot.Persona != "" {
		prompt += "\n\nPersona:\n" + bot.Persona
	}
	return prompt
}

// _repoWork ends the repo block: providers may not load a tool's description until they search
// for it, so the tools a change needs are named here.
const _repoWork = "Before your first edit, call start_work and work in the path it returns. " +
	"After you open the MR, call track_mr with its URL."

func cut(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit] + "…"
}

func messagePrompt(
	text string, items []memory.Item, counts memory.Counts, thread store.Thread, repo store.Repo,
	cwd string,
) string {
	var blocks []string
	if len(items) > 0 {
		var b strings.Builder
		b.WriteString("Memory items:")
		for _, item := range items {
			fmt.Fprintf(&b, "\n- [%s] %s (%s): %s",
				item.ID, item.Kind, item.Scope, cut(item.Body, _bodyCut))
			if item.Why != "" {
				fmt.Fprintf(&b, "\n  Why: %s", cut(item.Why, _whyCut))
			}
		}
		blocks = append(blocks, b.String())
	}
	if thread.Session == "" {
		blocks = append(blocks, fmt.Sprintf("Memory: repo %d, user %d, bot %d. Use recall.",
			counts.Repo, counts.User, counts.Bot))
		if repo.ID != "" {
			path, branch := thread.Worktree, thread.Branch
			if path == "" {
				path = cwd
			}
			if branch == "" {
				branch = "detached"
			}
			blocks = append(blocks, fmt.Sprintf("Repo: %s (%s)\nWorktree: %s\nBranch: %s\n%s",
				repo.Name, repo.ID, path, branch, _repoWork))
		}
	}
	blocks = append(blocks, "Message:\n"+text)
	return strings.Join(blocks, "\n\n")
}
