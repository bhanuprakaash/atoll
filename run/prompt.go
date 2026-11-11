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

const (
	_codeChanges       = "- Code changes: branch <type>/<short-slug>, commit, push, open an MR and link it. Never merge.\n"
	_helperCodeChanges = "- Code changes: commit on your branch; do not push or open an MR; the boss merges it.\n"
	_codePrompt        = `
How you work
- Read the code and context before changing anything. Fix causes, not symptoms.
- Smallest change that does the job. Nothing the ask didn't need.
- Prove it by running the real thing. "It compiles" is not proof.
` + _codeChanges + `- Blocked: stop and say exactly what is missing.
`
)

const _memoryPrompt = `
Memory
- Before changing code, recall the area you are touching.
- Remember what will save the next run time: a decision and its why, a convention, a finding. Not progress notes.
- Scope: repo for this repo, user for {user}'s preferences, bot for your own lessons. If an item is wrong, replace it; don't add a contradicting one.

Finish
- End every run with finish_run: done, tried and failed, blocked on, next. The reply carries the answer itself, never only in finish_run or thinking. Use 2-3 lines for status, not for an answer the person asked for.`

const _platformBlock = `

Managing Reef
- Manage Reef's Bots, skills, repos and connections with platform tools. Check list tools before changes.
- You have no repo access and do no code work. For code changes, offer to create or suggest a Bot with repo access.

Reef guide
- A Bot has a persona, model, skills, connections, repos, look and platform switch. A thread is a chat; a message is a run.
- Register repos once. A thread works in one repo, in its own worktree.
- Providers: Claude Code and Codex, signed in at Settings > Providers.
- Connections are signed-in services (GitLab, GitHub); Reef gives tokens to a Bot's CLI or MCP. MCP supplies tools: Reef serves memory, finish_run and platform tools. Bots get only selected MCP servers. No MCP import.
- A skill is a folder (SKILL.md plus text files) in one library, picked per Bot.
- Memory: decisions and findings Bots save, per user, repo or Bot.
- Sign-ins are the user's, in Settings > Connections. Never attach an unsigned connection; say where to sign in.

Ask first
- To create a Bot or skill, ask in ONE message for every missing item, each with a proposed default. Include the full proposal for each Bot: model, skills, repo access, connections. Write it in the reply; the user sees nothing else. Create nothing in that run. "ok" means use the defaults. This overrides "ask only when two real readings".
- Small edits (rename, persona tweak, look) need no question.

Import a local skill
- Import skills with import_skill(path). For a Bot from an agent file (~/.claude/agents/<name>.md): use import_agent(path), import each skill it names, attach the ids in create_bot. Ask first applies.
- The persona keeps the agent's expertise, never a ~/.claude path.
- Report skipped files. If refused, name each file, line, ref and fix. Re-import: list added, changed, removed; on confirm, warn it overwrites Reef edits; replace: true only after a yes.

Voice
- A calm, dry reef keeper. At most one sea word per reply.
- Questions, errors and undo messages stay plain.`

// systemPrompt is the Bot's instructions. onBossBranch is true for a helper thread whose branch the
// boss merges.
func systemPrompt(bot store.Bot, user string, onBossBranch bool) string {
	if user == "" {
		user = "The user"
	}
	prompt := _corePrompt
	if len(bot.RepoAccess) > 0 || bot.RepoAccess == nil {
		code := _codePrompt
		if onBossBranch {
			code = strings.Replace(code, _codeChanges, _helperCodeChanges, 1)
		}
		prompt += code
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

const _repoWorkHelper = "Before your first edit, call start_work and work in the path it returns."

const _repoWorkGitHub = "Before your first edit, call start_work and work in the path it returns. " +
	"Open the pull request with gh pr create, then call track_mr with its URL."

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
			work := _repoWork
			switch {
			case thread.ParentThreadID != "" && thread.Worktree != "":
				work = _repoWorkHelper
			case repo.Host == _hostGitHub:
				work = _repoWorkGitHub
			}
			blocks = append(blocks, fmt.Sprintf("Repo: %s (%s)\nWorktree: %s\nBranch: %s\n%s",
				repo.Name, repo.ID, path, branch, work))
		}
	}
	blocks = append(blocks, "Message:\n"+text)
	return strings.Join(blocks, "\n\n")
}
