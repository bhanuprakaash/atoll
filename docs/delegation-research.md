# Delegation research

How other products and frameworks let one agent hand work to another, and what that means for Reef's delegation rework. Read from first-party docs, specs and source on 2026-11-24. Each claim links to the page it came from. The last section lists what could not be confirmed. Everything under "What this suggests for Reef" is a suggestion, not a decision.

## Short answer

- The industry default is the shape Reef already has. One lead hands a task with a written brief to a worker that has its own context, and gets one result back. Anthropic recommends starting there, LangChain now recommends it over its own supervisor library, and Cognition's 2026 follow-up favours the same manager and child shape, which is live in Devin and which Cognition says is still in development. [Anthropic patterns](https://claude.com/blog/multi-agent-coordination-patterns) · [LangGraph supervisor](https://raw.githubusercontent.com/langchain-ai/langgraph-supervisor-py/main/README.md) · [Cognition 2026](https://cognition.com/blog/multi-agents-working)
- "One report back" is not enough on its own. The systems that last add a typed "needs input" state to the task. The worker pauses, the question goes out, and the answer resumes the same task with the same id. A2A calls it `input-required`, LangGraph `interrupt`, the OpenAI Agents SDK `interruptions`, Devin needs input. Who gets the question differs. A2A, Devin, CrewAI and Claude Code teams can send it to the parent agent. LangGraph, the OpenAI Agents SDK, ADK task mode and Codex send it to the person or the calling app. Reef has the mechanism (a helper's question is its final reply, the boss answers with `continue`) but no state that tells a question from a finished report.
- The durable versions of "ask the parent" all pause the worker and resume it with a new message on the same task. The blocking version, CrewAI's ask tool, is a plain synchronous call with no stated behaviour on a crash.
- For a team, products converge on a fixed lead, a shared task list with a few states, and direct named messages into a per-agent mailbox. Claude Code agent teams ship this and still call it experimental. Free group chat (AutoGen) needs a speaker selector and hard stop conditions, and AutoGen is archived. [Agent teams](https://code.claude.com/docs/en/agent-teams) · [AutoGen README](https://raw.githubusercontent.com/microsoft/autogen/main/README.md)
- No source promises exactly-once delivery. A2A says webhooks get at least one attempt, duplicates may happen, clients should be idempotent, and messages are not a reliable channel for critical information. Reliability comes from durable task state that can be read again, not from the message. [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)
- Most systems cap depth, concurrency or rounds, though OpenAI Swarm and ADK's loop have no cap by default. Magentic-One and Agent Framework also count rounds without progress and force a replan. Reef's depth 1, limit of 3 and "stuck twice in a row" are in line with this.
- Parallel agents that write the same code are the failure everyone reports. Cognition's rule is to keep writes single-threaded and let extra agents add analysis. Reef's one branch per helper with the boss merging follows that rule. [Cognition 2026](https://cognition.com/blog/multi-agents-working)
- A message from another agent is never the person's consent. Claude Code enforces this for teammates and for cross-session messages, and A2A passes an authorization request up the chain to the client. Reef already sends approvals to the person. [Agent teams](https://code.claude.com/docs/en/agent-teams) · [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)
- What the person sees by default is a row or card per agent with a status, and the agent's transcript on demand. Only Claude Code's cross-session messaging documents a one-line preview per agent-to-agent message that expands to the full text. No product read puts the whole exchange in the main chat. [Cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging)

## 1. Communication between Bots

The question: is "give a task, get one report" enough, and how does a sub-agent ask its parent something mid-task?

### What each source does

**A2A (Google, now a2aproject), spec 1.0.0.** A task has eight states: submitted, working, completed, failed, canceled, rejected, input-required, auth-required. The first two are in progress, the next four are terminal, the last two are "interrupted". An agent asks for input by moving the task to `input-required`. The client answers by sending a new message with the same `taskId` and `contextId`. A blocking send returns when the task reaches a terminal or an interrupted state, so a question ends the call the same way a result does. A message to a task in a terminal state is rejected with `UnsupportedOperationError`. Messages carry communication, artifacts carry results, and the spec says results should not travel as messages. `auth-required` works the same way for credentials, and an agent that is itself a client may pass the request up to its own client. [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)

**Claude Code subagents.** A subagent returns only its final message to the caller. The only thing the parent passes down is the prompt string. `AskUserQuestion` is removed from every subagent's tools, so a subagent cannot ask the person. Permission prompts still reach the person in the main session, with the subagent named. The parent can send a running or finished subagent a message with `SendMessage`. The subagent treats it as task direction, corrections mid-task included, and a resumed subagent keeps its full history. A subagent the person stopped does not resume when the parent messages it. The message is refused and the parent is told the agent was cancelled. A named subagent that has `SendMessage` gets a roster that lists `main` and the other named agents as valid recipients. The page does not say that a running subagent can ask `main` a question and wait for the answer. [Subagents](https://code.claude.com/docs/en/sub-agents) · [Agent SDK subagents](https://code.claude.com/docs/en/agent-sdk/subagents)

**Claude Code agent teams.** Any teammate can message any other by name, and the lead too. A teammate that finishes and stops notifies the lead on its own, with its final answer in the notice. A teammate whose turn ends on an API error notifies the lead that it failed. Two request and reply pairs are typed: a plan approval request from teammate to lead, and a shutdown request from lead to teammate that the teammate may approve or reject. [Agent teams](https://code.claude.com/docs/en/agent-teams)

**Claude Code cross-session messaging.** A message is plain text one session writes to another. The receiver reads it between tool calls if it is mid-turn, or starts a new turn if idle. A sender can also ask for one notice when the other session next goes idle, and that subscription expires after 12 hours. [Cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging)

**Anthropic's research system (June 2025).** The lead runs subagents synchronously and waits for each batch. Anthropic names this a bottleneck. The lead cannot steer subagents, subagents cannot coordinate, and one slow subagent can block the whole system. They say asynchronous execution would add parallelism and also problems in result coordination, state consistency and error propagation. A brief needs an objective, an output format, guidance on tools and sources, and clear task boundaries. Without detailed task descriptions, agents duplicated work and left gaps. Subagents write large outputs to external storage and pass back a reference, so the result is not retold through the lead. [Research system](https://www.anthropic.com/engineering/multi-agent-research-system)

**OpenAI Agents SDK.** Two shapes. With agents as tools, a manager keeps control and calls a specialist through `Agent.as_tool()`. The nested agent gets one input string and does not inherit the parent's conversation. With a handoff, the specialist becomes the active agent for the rest of the turn and sees the whole previous conversation unless an input filter trims it. A nested agent that needs approval pauses the whole run, and the pending items appear in `result.interruptions`. The run resumes later from a saved `RunState`. [Multi-agent](https://openai.github.io/openai-agents-python/multi_agent/) · [Handoffs](https://openai.github.io/openai-agents-python/handoffs/) · [Tools](https://openai.github.io/openai-agents-python/tools/) · [Running agents](https://openai.github.io/openai-agents-python/running_agents/)

**Codex subagents.** Codex spawns subagents after a direct request or an instruction in `AGENTS.md` or a skill. It waits until all requested results are available and returns one consolidated response. An approval request from a subagent appears in the main view with the source thread's label. In non-interactive runs an action that needs a new approval fails and the error goes back to the parent. [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents.md)

**Google ADK.** A coordinator delegates to subagents, and each subagent has a mode. `single_turn` has no user interaction and returns its result on its own. `task` may ask the user clarifying questions and returns to the coordinator when it calls `finish_task`. `chat` talks freely and returns only by an explicit transfer. Task and single-turn agents each run in an isolated session branch and cannot see their peers. Task-mode agents must be leaves. [ADK collaboration](https://adk.dev/workflows/collaboration/index.md)

**LangGraph.** `interrupt()` pauses the graph and saves state through a checkpointer. `Command(resume=value)` on the same `thread_id` continues it, and the value becomes the return of `interrupt()`. An interrupt inside a subgraph or a tool pauses the parent too. On resume the node runs again from its first line, so the docs ask that side effects before the interrupt be idempotent. [Interrupts](https://docs.langchain.com/oss/python/langgraph/interrupts)

**CrewAI.** An agent with `allow_delegation=True` gets two tools: `Delegate work to coworker(task, context, coworker)` and `Ask question to coworker(question, context, coworker)`. Both are synchronous calls that return the coworker's answer. The docs list agents delegating back and forth without end as a known problem and fix it by turning delegation off for specialists. [CrewAI collaboration](https://docs.crewai.com/en/concepts/collaboration)

**Devin.** A coordinator session starts child sessions, each in its own VM. It is woken when a child finishes or needs input, so it does not poll. It can send instructions to a child mid-task, pause it or stop it. Cognition's 2026 post lists what still fails: managers that are too prescriptive without codebase context, agents that assume they share state with their children, and no good way for a child to tell its siblings about a discovery. [Devin docs](https://docs.devin.ai/work-with-devin/advanced-capabilities) · [Manage Devins](https://cognition.com/blog/devin-can-now-manage-devins) · [Cognition 2026](https://cognition.com/blog/multi-agents-working)

### Comparison

| Source                | Result back                   | Child asks mid-task                   | Parent answers            | Parent steers a running child       |
| --------------------- | ----------------------------- | ------------------------------------- | ------------------------- | ----------------------------------- |
| A2A                   | Artifacts on the task         | Task goes to `input-required`         | New message, same task id | New message on the task             |
| Claude Code subagents | Final message only            | Not documented                        | Not applicable            | `SendMessage`, read as direction    |
| Claude Code teams     | Idle notice with final answer | Direct message to lead                | Direct message back       | Direct message                      |
| OpenAI Agents SDK     | Tool result or handoff        | Only approvals, as `interruptions`    | Resume from `RunState`    | Not documented                      |
| Codex                 | One consolidated response     | Approvals only, to the person         | Person answers            | By asking Codex                     |
| ADK                   | Auto return or `finish_task`  | Task mode asks the user               | User answers              | Not documented                      |
| LangGraph             | Subgraph or tool return       | `interrupt()`                         | `Command(resume=...)`     | Not applicable                      |
| CrewAI                | Tool return                   | `Ask question to coworker`            | Synchronous tool return   | Not applicable                      |
| Devin                 | Child wakes coordinator       | Child "needs input" wakes coordinator | Message to child          | Message, pause, stop                |
| Reef today            | Report wakes the boss         | Question is the final reply           | `continue`                | `continue`, held until the run ends |

The table shows that Reef's flow matches A2A and Devin in mechanism. The missing piece is the state. In A2A and Devin the parent can tell "finished" from "waiting for you" without reading the text.

## 2. A team of Bots

The question: what models exist for a group that works together, and which have proven reliable?

### The models

**Parent and child tool call (agent as tool).** One agent calls another like a function and gets the result. Used by Claude Code subagents, OpenAI `as_tool`, ADK single-turn mode, Cursor subagents, CrewAI's delegate tool. LangChain's supervisor README now recommends building the supervisor with plain tool calls for most cases. LangChain's multi-agent guide says the centre of the design is deciding what each agent sees. [LangChain multi-agent](https://docs.langchain.com/oss/python/langchain/multi-agent) Anthropic says it fits the widest range of problems with the least coordination, and names its limit: the lead becomes an information bottleneck. [Anthropic patterns](https://claude.com/blog/multi-agent-coordination-patterns) · [LangGraph supervisor](https://raw.githubusercontent.com/langchain-ai/langgraph-supervisor-py/main/README.md) · [Cursor subagents](https://cursor.com/docs/subagents)

**Handoff.** Control of the conversation moves to another agent, which then answers the user. OpenAI Swarm introduced it as a function that returns an agent. The Agents SDK exposes it as a `transfer_to_<agent>` tool. LangGraph's swarm library remembers which agent was last active. It is a routing tool for one conversation, not a way to work in parallel. [Swarm](https://raw.githubusercontent.com/openai/swarm/main/README.md) · [Handoffs](https://openai.github.io/openai-agents-python/handoffs/) · [LangGraph swarm](https://raw.githubusercontent.com/langchain-ai/langgraph-swarm-py/main/README.md)

**Shared channel (group chat).** Every message goes to every member, and something picks the next speaker. AutoGen's `SelectorGroupChat` has a model pick the speaker, does not pick the same speaker twice in a row by default, and its own tutorial caps the chat at 25 messages to avoid an infinite loop. AutoGen lists eleven built-in termination conditions because a run can otherwise go on without end. The repo is now archived and points to Microsoft Agent Framework. [Selector group chat](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/selector-group-chat.html) · [Termination](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/termination.html) · [AutoGen README](https://raw.githubusercontent.com/microsoft/autogen/main/README.md)

**Group chat with a planning manager (Magentic-One).** An orchestrator keeps a task ledger (facts, guesses, the plan) and a progress ledger it fills each round: is the request satisfied, is the team in a loop, is progress being made, who speaks next, with what instruction. Rounds without progress add to a stall counter. Past the limit it resets and replans. Agent Framework exposes `max_round_count`, `max_stall_count` and `max_reset_count`, and an optional plan review by a human. Microsoft says it is untested outside the original four-agent design. [Magentic-One](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/magentic-one.html) · [Agent Framework Magentic](https://learn.microsoft.com/en-us/agent-framework/workflows/orchestrations/magentic)

**Mailbox and message passing.** Each agent has an inbox, and agents address each other by name. AutoGen Core has direct messages by agent id and broadcast to a topic with subscriptions. Claude Code teams give each agent a JSON mailbox file. A message counts as sent only when the write to the recipient's file succeeds, and a failed write returns an error to the sender. Claude Code has no broadcast: to reach everyone the sender writes one message per recipient. [AutoGen topics](https://microsoft.github.io/autogen/stable/user-guide/core-user-guide/core-concepts/topic-and-subscription.html) · [Agent teams](https://code.claude.com/docs/en/agent-teams)

**Shared task list.** Claude Code teams keep one list with three states: pending, in progress, completed. A task may depend on others and shows as blocked until they finish. The lead assigns tasks or teammates claim the next free one, and claiming takes a file lock so two cannot take the same task. The docs name a weak point. Teammates sometimes fail to mark a task completed, which blocks its dependents. [Agent teams](https://code.claude.com/docs/en/agent-teams)

**Shared state (blackboard).** Agents read and write one store with no coordinator. Anthropic's description of the failure: agents duplicate work, and reactive loops keep spending tokens without converging. Their fix is a stop condition designed in from the start: a time budget, no new findings for N cycles, or one agent that decides the answer is good enough. [Anthropic patterns](https://claude.com/blog/multi-agent-coordination-patterns)

### What Claude Code agent teams fix in place

This is the closest shipped product to "a team of Bots", so its limits are worth copying or avoiding on purpose. The lead is fixed for the session. Teammates cannot spawn teammates. A session has one team. Teammates start with the spawn prompt and project context, not the lead's history. Permission prompts from teammates go to the person in the lead's session, and a teammate cannot approve for the person or relay a denied action to another teammate. The docs recommend 3 to 5 teammates, 5 to 6 tasks each, and one owner per file because two teammates editing one file overwrite each other. In-process teammates are not restored when a session is resumed. The feature is off by default and marked experimental. [Agent teams](https://code.claude.com/docs/en/agent-teams)

### Published cautions

- Cognition, June 2025: share full context, not single messages, and remember that every action carries decisions the other agent cannot see. Their example is two subagents building parts of one game that do not fit together. [Cognition 2025](https://cognition.com/blog/dont-build-multi-agents)
- Cognition, April 2026: the 2025 points still hold for swarms of parallel writers. What works is a manager that splits, children that execute, and a manager that combines, with writes kept single-threaded. It dismisses arbitrary networks of agents negotiating with each other. [Cognition 2026](https://cognition.com/blog/multi-agents-working)
- Anthropic, June 2025: multi-agent runs used about 15 times the tokens of a chat. Early leads spawned 50 subagents for a simple query. The approach fits wide, parallel research and fits poorly where all agents need the same context, which includes most coding. [Research system](https://www.anthropic.com/engineering/multi-agent-research-system)
- Anthropic, January 2026: split work by context, not by role. A planner, implementer, tester and reviewer chain loses detail at each handoff. A verifier works because it needs little context, and its failure is declaring success after one or two passing tests. [When to use multi-agent](https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them)

### Comparison

| Model            | Who uses it                              | Holds up when                               | Fails by                               |
| ---------------- | ---------------------------------------- | ------------------------------------------- | -------------------------------------- |
| Agent as tool    | Claude Code, OpenAI, ADK, Cursor, CrewAI | Tasks are independent, lead owns the answer | Lead becomes a bottleneck, thin briefs |
| Handoff          | OpenAI, LangGraph swarm                  | Routing one conversation                    | Not parallel, history grows            |
| Group chat       | AutoGen                                  | Small, capped discussions                   | Loops, cost, needs stop rules          |
| Planning manager | Magentic-One                             | Open tasks with a stall counter             | Untested beyond its original team      |
| Mailbox          | Claude Code teams, AutoGen Core          | Named recipients, durable write             | Chatter and reply loops                |
| Shared task list | Claude Code teams                        | Tasks have one owner and clear states       | Status not updated, dependents block   |
| Shared state     | Anthropic's description                  | A stop condition exists                     | Duplicate work, reactive loops         |

## 3. Reliability

### Delivery

- A2A is the only source that states delivery rules, and it does not promise much. Push notifications get at least one attempt per webhook. Retries are optional in one section and recommended in another. Clients should handle duplicates. Stream events must arrive in the order they were generated, but a client that reconnects may miss status updates, and "messages MUST NOT be considered a reliable delivery mechanism for critical information". The durable thing is the task, which the client can fetch again. [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)
- A2A on idempotency: sending a message may be idempotent, and an agent may use `messageId` to detect duplicates. Cancel is idempotent. [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)
- Claude Code teams: a message is sent when the write to the recipient's mailbox file succeeds, and the sender gets an error otherwise. Malformed mailbox entries are reported and removed, valid ones still deliver. Nothing is said about exactly-once. [Agent teams](https://code.claude.com/docs/en/agent-teams)
- AutoGen Core: a published message goes to each subscribed agent once per publish, and a topic with no subscription delivers to nobody. Nothing is said about crashes. [AutoGen topics](https://microsoft.github.io/autogen/stable/user-guide/core-user-guide/core-concepts/topic-and-subscription.html)
- OpenAI Agents SDK, Codex, CrewAI, ADK, Cursor, Devin: no delivery guarantee stated on the pages read.

### Task state, crash and restart

- A2A: the eight states above. Terminal tasks accept no more messages. A new request in the same `contextId` starts a new task that may reference the old one. [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)
- LangGraph: a checkpointer saves state at each super-step. If a node fails, writes from nodes that finished in the same step are kept and not run again. Three durability modes trade speed for safety: `exit` saves only when the run ends, `async` saves while the next step runs, `sync` saves before the next step starts. Resume after an interrupt runs the node again from the top. [Checkpointers](https://docs.langchain.com/oss/python/langgraph/checkpointers.md) · [Interrupts](https://docs.langchain.com/oss/python/langgraph/interrupts)
- Anthropic's research system: agents are stateful and errors compound, so they resume from where the error happened and do not restart from the beginning, with retry logic and regular checkpoints. [Research system](https://www.anthropic.com/engineering/multi-agent-research-system)
- Claude Code: subagent transcripts are stored per agent and survive compaction of the main conversation. A subagent that hits `maxTurns` returns output marked partial and can be resumed. An API error that ends a subagent is never delivered as its result. In-process teammates are lost on session resume, and the lead may then message teammates that no longer exist. [Subagents](https://code.claude.com/docs/en/sub-agents) · [Agent SDK subagents](https://code.claude.com/docs/en/agent-sdk/subagents) · [Agent teams](https://code.claude.com/docs/en/agent-teams)
- OpenAI Agents SDK: no durability of its own. Its docs point to Temporal, Dapr, Restate and DBOS for runs that must survive long waits and restarts. [Running agents](https://openai.github.io/openai-agents-python/running_agents/)
- Temporal, as the pattern those integrations follow: a child workflow has its own recorded history, and a parent close policy decides what happens to children when the parent closes. The default is terminate, and abandon lets the child keep running. Workflow id and update id serve as idempotency keys. Temporal's own advice is to use an Activity when in doubt, which is the same lesson as starting with one agent. [Child workflows](https://docs.temporal.io/child-workflows) · [Parent close policy](https://docs.temporal.io/parent-close-policy) · [Sending messages](https://docs.temporal.io/sending-messages)

### Cancellation and timeouts

- A2A: cancel is a request. The server tries, success is not guaranteed, and a terminal task returns `TaskNotCancelableError`. [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)
- Claude Code teams: shutdown is a request the teammate may reject with a reason, and it finishes its current tool call first. The person can stop a teammate from the panel. [Agent teams](https://code.claude.com/docs/en/agent-teams)
- GitHub Copilot: steering input is applied after the current tool call finishes. Stopping a session keeps commits already pushed. [Copilot sessions](https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/manage-and-track-agents)
- LangGraph: per-node timeouts and retry policies with backoff. A timeout is retried by default. [Fault tolerance](https://docs.langchain.com/oss/python/langgraph/fault-tolerance.md)
- AutoGen: `TimeoutTermination`, `TokenUsageTermination`, `MaxMessageTermination`, and `ExternalTermination` for a stop from outside. [Termination](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/termination.html)

### Limits and loop control

| Source                        | Depth                                    | Concurrency                                        | Rounds or turns                                | Loop and stall control                           |
| ----------------------------- | ---------------------------------------- | -------------------------------------------------- | ---------------------------------------------- | ------------------------------------------------ |
| Claude Code subagents         | 3 layers by default, 1 turns nesting off | 20 at once, the error tells the model not to retry | `maxTurns` per subagent                        | Spend cap `maxBudgetUsd` stops spawning          |
| Claude Code teams             | No nested teams                          | No hard limit, 3 to 5 advised                      | Not stated                                     | Hooks can reject an idle or a task completion    |
| Claude Code cross-session     | Not applicable                           | Queue of 50 accepted messages                      | Sending pauses after a long exchange           | Rate limit per sender, identical repeats dropped |
| Cursor subagents              | A child of a child cannot spawn          | 2 to 3 advised                                     | Not stated                                     | Not stated                                       |
| Magentic (Agent Framework)    | One manager                              | One speaker per round                              | `max_round_count`                              | `max_stall_count`, `max_reset_count`             |
| Magentic-One (AutoGen source) | One orchestrator                         | One speaker per round                              | `max_turns` 20                                 | `max_stalls` 3                                   |
| AutoGen group chat            | Flat                                     | One speaker per turn                               | Termination conditions                         | No repeat speaker by default                     |
| LangGraph                     | Nested graphs allowed                    | Per graph                                          | Recursion limit, about 10,000 super-steps      | `GraphRecursionError`                            |
| OpenAI Agents SDK             | Not stated                               | Not stated                                         | `max_turns`, `MaxTurnsExceeded`                | Not stated                                       |
| OpenAI Swarm                  | Not stated                               | Not stated                                         | `max_turns` defaults to infinite               | None                                             |
| ADK                           | Task-mode agents are leaves              | Only single-turn runs in parallel                  | `LoopAgent` `max_iterations`, unset by default | `escalate` ends a loop                           |
| CrewAI                        | Specialists with delegation off          | Not stated                                         | Not stated                                     | Clear hierarchy                                  |
| Reef today                    | 1                                        | 3 helper runs per boss                             | Idle limit per run                             | "Stuck twice in a row", wake window              |

Sources for the table: [Subagents](https://code.claude.com/docs/en/sub-agents) · [Agent SDK subagents](https://code.claude.com/docs/en/agent-sdk/subagents) · [Agent teams](https://code.claude.com/docs/en/agent-teams) · [Cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging) · [Cursor subagents](https://cursor.com/docs/subagents) · [Agent Framework Magentic](https://learn.microsoft.com/en-us/agent-framework/workflows/orchestrations/magentic) · [Magentic-One source](https://raw.githubusercontent.com/microsoft/autogen/main/python/packages/autogen-agentchat/src/autogen_agentchat/teams/_group_chat/_magentic_one/_magentic_one_group_chat.py) · [Selector group chat](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/selector-group-chat.html) · [LangGraph graph API](https://docs.langchain.com/oss/python/langgraph/graph-api.md) · [Running agents](https://openai.github.io/openai-agents-python/running_agents/) · [Swarm](https://raw.githubusercontent.com/openai/swarm/main/README.md) · [ADK collaboration](https://adk.dev/workflows/collaboration/index.md) · [ADK loop](https://adk.dev/agents/workflow-agents/loop-agents/index.md) · [CrewAI collaboration](https://docs.crewai.com/en/concepts/collaboration)

The clearest warning on chatter is in Claude Code's own docs. Two sessions that answer each other slowly enough to pass the rate limit "can exchange thousands of messages without a prompt from you". Fast loops are throttled. Slow loops need a budget. [Cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging)

## 4. What the person sees

### What each product does

**Claude Code subagents.** The delegation is one tool call row in the main chat. Running subagents show as rows in a panel below the prompt, as a tree, with a `(+N)` count on a row that has children. When a subagent finishes, its row goes away and a footer hint points to `/tasks` for 30 seconds. Enter on a subagent opens its transcript. Only the summary enters the main conversation. [Subagents](https://code.claude.com/docs/en/sub-agents)

**Claude Code agent teams.** One row per teammate in the same panel. Enter opens a teammate's transcript and lets the person type to it. Idle rows hide 30 seconds after everything is idle. More than three idle teammates collapse into one `N idle agents` row. Working and failed teammates always keep a row. Ctrl+T shows the task list. Split panes show every teammate at once for people who want that. [Agent teams](https://code.claude.com/docs/en/agent-teams)

**Claude Code cross-session messages.** This is the closest match to "this agent asked this". An incoming message shows as one dim line with the sender and the first line of the text, such as `› Message from @api-worker: Schema migration finished (ctrl+o to expand)`. The line stays in the conversation. The full text is in the transcript viewer, or a click expands it in place. A message that has not reached the model yet is listed under `Waiting to be delivered`. The model always reads the full message. The preview only shortens what the person sees. [Cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging)

**Codex.** The main thread shows subagent activity. The person opens a subagent's thread from that activity, or switches threads with `/subagents` in the CLI. The IDE panel shows each subagent's status and a stop-all control. An approval from a background thread shows the source thread's label, and `o` opens that thread before the person decides. [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents.md)

**Cursor.** A task card for the subagent in the parent chat. The parent gets only the final message. [Cursor subagents](https://cursor.com/docs/subagents)

**Devin.** Each managed Devin has its own session link, so the person can inspect its work or message it directly. The coordinator compiles the results. [Manage Devins](https://cognition.com/blog/devin-can-now-manage-devins)

**GitHub Copilot.** An agents panel lists sessions. A session opens to a log with the agent's reasoning, the tools it used and the tests it ran, plus token use and session length. [Copilot sessions](https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/manage-and-track-agents)

**Microsoft Agent Framework Magentic.** By default only the manager's final answer comes out. Per-agent output is opt-in through `intermediate_output_from`. Plan created, replanned and progress ledger events are separate from the answer. [Agent Framework Magentic](https://learn.microsoft.com/en-us/agent-framework/workflows/orchestrations/magentic)

**A2A.** The protocol's "opaque execution" principle says agents share declared results, not their internal thoughts, plans or tools. A client sees task state, status messages and artifacts. [A2A spec](https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md)

### Comparison

| Product                   | In the main chat by default                | Agent-to-agent messages      | Full detail             | Person can step in             |
| ------------------------- | ------------------------------------------ | ---------------------------- | ----------------------- | ------------------------------ |
| Claude Code subagents     | One tool call row, then the summary        | Not documented               | Transcript per subagent | Stop, approve                  |
| Claude Code teams         | Panel row per teammate, idle rows collapse | Not documented               | Transcript per teammate | Message, stop, approve         |
| Claude Code cross-session | One dim line per message                   | One-line preview with expand | Transcript viewer       | Approve or deny a held message |
| Codex                     | Activity in the main thread                | Not documented               | Subagent thread         | Approve, stop, steer           |
| Cursor                    | Task card                                  | Not documented               | Not documented          | Not documented                 |
| Devin                     | Coordinator's compiled result              | Not documented               | Session link per child  | Message a child                |
| Copilot                   | Sessions list                              | Not applicable               | Session log             | Steer, stop                    |
| Reef today                | "Handed to X" link, Helpers panel          | Not shown                    | Helper thread           | Stop, approve                  |

## 5. Patterns that keep showing up

| Pattern                      | One-line definition                                                                                        | Who uses it                                                                               |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Agent as tool                | The lead calls a worker like a function and gets one result                                                | Claude Code, OpenAI Agents SDK, ADK, Cursor, CrewAI                                       |
| Brief                        | The task text carries objective, output format, tools and boundaries, because the worker sees nothing else | Anthropic research system, Claude Code, Cursor                                            |
| Task with lifecycle states   | Work is a record with a state, and terminal states accept no more messages                                 | A2A, Claude Code task list, Temporal                                                      |
| Needs-input state            | The worker pauses with a question for its caller or the person, and the answer resumes the same task       | A2A `input-required`, LangGraph `interrupt`, OpenAI `interruptions`, Devin, ADK task mode |
| Escalate up the chain        | A request the receiver cannot grant goes to its own caller, and consent comes only from the person         | A2A `auth-required`, Claude Code permission prompts, Codex approvals                      |
| Completion notice            | The worker's end wakes the lead, so the lead does not poll                                                 | Claude Code teams, Devin, Codex, Reef                                                     |
| Mailbox                      | A durable inbox per agent, addressed by name, read between steps or at the next turn                       | Claude Code teams, AutoGen Core, Reef `inbox`                                             |
| Shared task list             | One list with owner, state and dependencies, claimed under a lock                                          | Claude Code teams                                                                         |
| Handoff                      | The conversation moves to another agent, which answers the user                                            | OpenAI Swarm and Agents SDK, LangGraph swarm, ADK chat mode                               |
| Group chat with a selector   | All see all messages, and a selector picks who speaks                                                      | AutoGen, Agent Framework                                                                  |
| Ledger and stall counter     | The lead records plan and progress each round, and N rounds without progress force a replan                | Magentic-One, Agent Framework                                                             |
| Checkpoint and resume        | State is saved at each step, and a restart continues from the last saved step                              | LangGraph, Temporal, Anthropic research system                                            |
| Idempotent receive           | Messages have ids, and the receiver tolerates duplicates                                                   | A2A, Temporal                                                                             |
| Single writer                | Only one agent writes a given branch or file, others advise                                                | Cognition, Claude Code teams' file ownership advice, Reef                                 |
| Budget caps                  | Depth, concurrency, turns, tokens or money are capped, and the cap returns a clear error                   | Claude Code, Agent Framework, LangGraph, AutoGen                                          |
| Opaque worker, visible state | The lead and the person see state and results, and the transcript is on demand                             | A2A, Claude Code, Codex, Devin, Copilot                                                   |

## 6. What this suggests for Reef

All of this is suggestion. Each item starts from what `docs/design.md` (Delegation) already has and names the change. Nothing here reopens a settled decision without saying so.

### (a) A helper asking its boss a question mid-task

Today a helper's question is its final reply. The run ends as `done`, the boss is woken with `{Helper} finished thread {id}:`, and the boss answers with `continue`. The pause is already durable, because the helper's session, worktree and the held message all survive a restart. This is the A2A `input-required` flow without the state.

Suggested, small now:

1. Add a `needs_input` outcome for a helper run, next to `done`, `failed`, `stopped`, `limited` and `stuck`. The helper sets it with one tool call, for example `ask_boss(question)`, that records the question and ends the run. Why: the boss, `helpers()` and the Helpers panel can then tell "finished" from "waiting for you" from data, the same way the background-run guard reads the `notice` field and never the text.
2. Give that outcome its own wake text, for example `{Helper} asks in thread {id}:` then the question, then `Answer with continue.` Keep `continue` as the only way to answer. Why: A2A answers on the same task id, and Reef's `continue` already resumes the same session and worktree.
3. Decide whether a question skips the 20 s wake window. A question blocks a helper slot, so waking the boss at once is reasonable. Reports can keep the window.
4. Add one line to the helper's first message that says when to ask and when to decide alone. Why: Cognition found that a child does not write back to its manager unless it is taught to, and Anthropic found vague briefs cause most duplicated work.
5. Keep the rule that the boss answers what it can and uses `tell_user` for what only the person can decide. Why: A2A passes `auth-required` up to the client, and Claude Code never lets an agent's message stand in for the person.

Not suggested now is a blocking `ask_boss` that keeps the helper's run open until the boss answers. It holds a provider session and a helper slot for an unknown time, needs a timeout, and is lost on restart. LangGraph's version of this re-runs the node on resume and asks for idempotent side effects. Anthropic lists state consistency and error propagation as the cost of going asynchronous inside a run.

One thing to leave room for is mid-run delivery of a boss's `continue`. Claude Code cross-session messaging delivers between tool calls and Copilot after the current tool call. Reef holds the message until the helper's run ends. Changing that depends on whether each provider accepts input during a run, which this research did not check.

### (b) A future team of Bots

Today Reef has one boss thread, helper threads with `parent_thread_id`, an `inbox` table that holds messages per thread, `from_bot_id` on each message, depth 1, and a branch per helper that the boss merges.

Suggested direction, when teams are built:

1. A fixed lead, members that cannot add members, one team per lead thread. This is Claude Code's shape, and it is Reef's depth 1 with peer messages added.
2. A shared task list, each task with an owner, a state and optional dependencies, claimed inside one SQLite transaction. Reef has no task record today. A delegation is a thread.
3. Direct messages addressed to a named member, stored in the existing inbox. No broadcast and no group chat. Claude Code has no broadcast, and AutoGen's group chat needs a selector and stop rules to stay bounded.
4. One writer per branch stays the rule. Members that review or investigate can run in parallel. Members that write get separate branches and the lead merges, as now.
5. A stop rule from day one: a round or message budget per team task, and a no-progress counter in the lead's wake text, as Magentic's stall counter and Reef's "stuck twice in a row" do.

Small now, to keep the door open without building a team:

- Treat each delegation as a task with an id and a state (`working`, `needs_input`, `done`, `failed`, `stopped`) stored apart from the run's own state. Today the helper's state is the last run's state.
- Store sender, recipient thread, kind and an optional "in reply to" on every Bot-to-Bot message, so a peer message later is the same row with a different sender. Messages already carry `from_bot_id` and a kind.
- Keep the report as "result plus a reference". A helper's work is already on a branch, so the report can stay short and point to the branch, which is Anthropic's advice to pass a reference and not the content.

Only for teams, not now: the shared task list UI, peer messaging tools for helpers, claiming, and dependencies.

### (c) Reliability rules

Reef already has a durable inbox that survives restart, runs marked `failed` on restart with one wake stored in the same transaction, depth 1, a limit of 3 helper runs, an idle limit with a stuck notice, refusals as tool results, a Stop that holds messages, and approvals that go to the person.

Suggested additions, each small:

1. Give every Bot-to-Bot message an id and mark it delivered in the same transaction that starts the run that carries it. State the rule in `docs/design.md` as "each message is delivered to exactly one run, or dropped with a reason". Why: no source gets exactly-once from the transport. They get it from a durable record plus an idempotent receiver.
2. Make `delegate` and `continue` safe to repeat. A provider can retry a tool call, and a duplicate `delegate` starts a second helper. A short dedupe window on (boss thread, Bot, brief text) or a key the tool returns would cover it. Why: A2A lets agents use `messageId` to detect duplicates, and Temporal uses workflow and update ids as idempotency keys.
3. Cap rounds per delegation. Count boss-to-helper `continue` messages and helper questions on one task, and after N with no new commit tell the boss to stop or tell the person. Why: Claude Code documents slow reply loops of thousands of messages, and Magentic resets after `max_stall_count`.
4. Give `needs_input` a clock. If the boss has not answered within a set time, the person sees it. Why: Claude Code's idle-notice subscription expires after 12 hours so nothing waits for ever.
5. Say what each state accepts. A closed helper thread drops messages today. Write the same table for `needs_input` (accepts `continue`), `stopped` (held until `continue`), `failed` and `limited`. Why: A2A names which states accept messages and returns an error for the rest.
6. Keep cancel as "request, then confirm". `stop_helper` already returns a result and stops within 5 s. Keep its answer idempotent when the helper is already stopped, which it is.

### (d) What the person sees

Today the person sees a "handed to X" link in the boss's chat, a Helpers panel, reports hidden from the boss's chat (settled after #396, where posting reports made the person read each one twice), `tell_user` for what the person must act on, and a read-only helper thread with the full exchange.

Suggested, as a new decision with a reason and not a reversal of #396:

1. By default, one line per question and answer pair in the boss's chat, collapsed. For example `Builder asked Lead: which migration tool?` and under it `Lead answered`. Clicking expands the full text or opens the helper thread at that message. Why: the owner wants to see "this agent asked this", and Claude Code's one-line preview with expand is the shipped pattern for this. Reports stay hidden as now. A question is rarer than a report and often carries a decision the person would want to know was made.
2. In the Helpers panel, show `waiting for {Boss}` as a state of its own, with the question's first line. Do not colour it red. It is not a failure.
3. On demand, the helper thread stays the full record. Add nothing else.
4. A helper's question waits on the boss, not on the person, so it is not a pending item for the person. It becomes one only when the boss calls `tell_user` or the clock in (c) 4 runs out. This keeps "one pending item shows in every place it matters, never twice".
5. If the line in the boss's chat turns out noisy, the fallback is Codex's shape: no line in the chat, and a count on the Helpers panel that opens the exchange.

### Small now, later, and not suggested

| Change                                              | Size            | Depends on                               |
| --------------------------------------------------- | --------------- | ---------------------------------------- |
| `needs_input` outcome, `ask_boss`, its wake text    | Small           | Nothing                                  |
| "Waiting for boss" in Helpers panel and `helpers()` | Small           | `needs_input`                            |
| One-line question and answer row in the boss's chat | Small to medium | `needs_input`, a design decision         |
| Message ids and delivered-once rule written down    | Small           | A read of the inbox code                 |
| Idempotent `delegate` and `continue`                | Small           | Nothing                                  |
| Round cap per delegation                            | Small           | A task record or a counter on the thread |
| Task record apart from run state                    | Medium          | Schema change                            |
| Mid-run delivery of `continue`                      | Unknown         | Provider support, not researched         |
| Shared task list, peer messages, team UI            | Large           | Task record, a team design               |
| Group chat, broadcast, nested teams                 | Not suggested   | None                                     |

## Sources

All accessed 2026-11-24.

| URL                                                                                                                                                                          | What was taken from it                                                                                                                                                                                                                                       |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| https://raw.githubusercontent.com/a2aproject/A2A/main/docs/specification.md                                                                                                  | A2A 1.0.0: task states, terminal and interrupted states, answering `input-required`, messages and artifacts, blocking and `return_immediately`, idempotency, cancel, stream order, push notification delivery, `auth-required`, agent card, opaque execution |
| https://code.claude.com/docs/en/agent-teams                                                                                                                                  | Agent teams: lead, teammates, mailbox files, task list states and locking, direct messages, idle notices, plan approval, shutdown, panel UI, permissions, limits, experimental status                                                                        |
| https://code.claude.com/docs/en/sub-agents                                                                                                                                   | Subagents: nesting depth, concurrency limit, `maxTurns`, `AskUserQuestion` removed, `SendMessage` and resume, stopped subagents, what returns, panel UI, transcript files                                                                                    |
| https://code.claude.com/docs/en/agent-sdk/subagents                                                                                                                          | Final message only returns, prompt string is the only input, background default, `parent_tool_use_id`, depth, concurrency and spend caps, API errors                                                                                                         |
| https://code.claude.com/docs/en/cross-session-messaging                                                                                                                      | Delivery between tool calls, treatment of incoming messages, one-line preview UI, idle notice and 12 hour expiry, loop throttles and limits                                                                                                                  |
| https://www.anthropic.com/engineering/multi-agent-research-system                                                                                                            | Brief contents, synchronous subagents and the bottleneck, token cost, resume from errors, outputs by reference, early failures, poor fits                                                                                                                    |
| https://claude.com/blog/multi-agent-coordination-patterns                                                                                                                    | Five patterns, their limits, the recommendation to start with orchestrator and subagent, termination conditions for shared state                                                                                                                             |
| https://claude.com/resources/articles/building-multi-agent-systems-when-and-how-to-use-them                                                                                  | When multi-agent wins, split by context, the telephone game, verifier pattern and its failure                                                                                                                                                                |
| https://cognition.com/blog/dont-build-multi-agents                                                                                                                           | Two principles, the game example, single-threaded agent advice (2025-06-12)                                                                                                                                                                                  |
| https://cognition.com/blog/multi-agents-working                                                                                                                              | What works in 2026, single-threaded writes, manager and child failure modes, open problems (2026-04-22)                                                                                                                                                      |
| https://cognition.com/blog/devin-can-now-manage-devins                                                                                                                       | Coordinator actions on managed Devins, session link per child (2026-03-19)                                                                                                                                                                                   |
| https://docs.devin.ai/work-with-devin/advanced-capabilities                                                                                                                  | Coordinator woken when a child finishes or needs input, pause and stop, auto-approve setting                                                                                                                                                                 |
| https://openai.github.io/openai-agents-python/multi_agent/                                                                                                                   | Agents as tools against handoffs, code against model orchestration                                                                                                                                                                                           |
| https://openai.github.io/openai-agents-python/handoffs/                                                                                                                      | Handoff as a `transfer_to_` tool, history seen by the new agent, input filters                                                                                                                                                                               |
| https://openai.github.io/openai-agents-python/tools/                                                                                                                         | `Agent.as_tool`: input, no inherited state, `needs_approval` and `interruptions`, `on_stream`, `max_turns`                                                                                                                                                   |
| https://openai.github.io/openai-agents-python/running_agents/                                                                                                                | Agent loop, `MaxTurnsExceeded`, durable execution integrations, resume from `RunState`                                                                                                                                                                       |
| https://raw.githubusercontent.com/openai/swarm/main/README.md                                                                                                                | Swarm status, handoff as a returned agent, stateless, `max_turns` default                                                                                                                                                                                    |
| https://learn.chatgpt.com/docs/agent-configuration/subagents.md                                                                                                              | Codex subagents: when spawned, consolidated result, approvals with thread label, `/subagents`, IDE panel, config keys                                                                                                                                        |
| https://raw.githubusercontent.com/microsoft/autogen/main/README.md                                                                                                           | AutoGen archived, pointer to Agent Framework, Core API description                                                                                                                                                                                           |
| https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/selector-group-chat.html                                                                          | Broadcast to all members, model picks the speaker, no repeat speaker, 25 message cap in the example                                                                                                                                                          |
| https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/termination.html                                                                         | Built-in termination conditions and why they exist                                                                                                                                                                                                           |
| https://microsoft.github.io/autogen/stable/user-guide/core-user-guide/core-concepts/topic-and-subscription.html                                                              | Direct messages, topics, subscriptions, once per recipient                                                                                                                                                                                                   |
| https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/magentic-one.html                                                                                 | Orchestrator, task ledger, progress ledger, outer and inner loop                                                                                                                                                                                             |
| https://raw.githubusercontent.com/microsoft/autogen/main/python/packages/autogen-agentchat/src/autogen_agentchat/teams/_group_chat/_magentic_one/_magentic_one_group_chat.py | Defaults `max_turns` 20 and `max_stalls` 3                                                                                                                                                                                                                   |
| https://learn.microsoft.com/en-us/agent-framework/workflows/orchestrations/magentic                                                                                          | Progress ledger fields, stall counter and reset, round, stall and reset limits, plan review, intermediate output opt-in                                                                                                                                      |
| https://docs.langchain.com/oss/python/langgraph/interrupts                                                                                                                   | `interrupt`, resume with `Command`, node restarts on resume, idempotent side effects, subgraphs and tools                                                                                                                                                    |
| https://docs.langchain.com/oss/python/langgraph/checkpointers.md                                                                                                             | Checkpoint per super-step, pending writes, durability modes                                                                                                                                                                                                  |
| https://docs.langchain.com/oss/python/langgraph/fault-tolerance.md                                                                                                           | Node timeouts and retry policy                                                                                                                                                                                                                               |
| https://docs.langchain.com/oss/python/langgraph/graph-api.md                                                                                                                 | Recursion limit and `GraphRecursionError`                                                                                                                                                                                                                    |
| https://docs.langchain.com/oss/python/langchain/multi-agent                                                                                                                  | Subagents, handoffs, skills and router patterns, context engineering                                                                                                                                                                                         |
| https://raw.githubusercontent.com/langchain-ai/langgraph-supervisor-py/main/README.md                                                                                        | Supervisor controls communication, output modes, note to use tools directly                                                                                                                                                                                  |
| https://raw.githubusercontent.com/langchain-ai/langgraph-swarm-py/main/README.md                                                                                             | Swarm handoffs, last active agent, checkpointer needed                                                                                                                                                                                                       |
| https://docs.crewai.com/en/concepts/collaboration                                                                                                                            | `allow_delegation`, the delegate and ask tools, delegation loops                                                                                                                                                                                             |
| https://adk.dev/workflows/collaboration/index.md                                                                                                                             | Coordinator and subagents, chat, task and single-turn modes, isolated branches, task agents as leaves                                                                                                                                                        |
| https://adk.dev/agents/workflow-agents/loop-agents/index.md                                                                                                                  | `LoopAgent`, `max_iterations`, `escalate`                                                                                                                                                                                                                    |
| https://cursor.com/docs/subagents                                                                                                                                            | Foreground and background, final message only, nesting limit, resume by id, task card, cost                                                                                                                                                                  |
| https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/manage-and-track-agents                                                                      | Agents panel, session log contents, steering after the current tool call, stop                                                                                                                                                                               |
| https://docs.temporal.io/child-workflows                                                                                                                                     | Child workflow definition, own history, advice to prefer an Activity when in doubt                                                                                                                                                                           |
| https://docs.temporal.io/parent-close-policy                                                                                                                                 | Terminate as default, abandon                                                                                                                                                                                                                                |
| https://docs.temporal.io/sending-messages                                                                                                                                    | Signals, queries, updates, ids as idempotency keys                                                                                                                                                                                                           |

## Not confirmed

- Codex defaults for subagent depth, thread count and job runtime. The official page says Codex chooses the thread default when unset and does not mention depth. Third-party pages give 6 threads, depth 1 and 1800 s, and one issue report contradicts them.
- Whether a Codex subagent can message its parent or a sibling. The page only says Codex routes follow-up instructions.
- How Cursor shows subagent activity beyond the task card, and whether a Cursor subagent can ask a question.
- How Devin shows child sessions in its UI beyond a session link per child, any cap on children, and how its internal MCP between manager and children works.
- GitHub Copilot session status values, and any agent-to-agent messaging in Agent HQ. Only the session management page was read.
- LangGraph's default durability mode. The durable execution URL now serves the persistence page, and the checkpointers page lists the three modes without naming the default.
- Whether an OpenAI Agents SDK handoff ever returns control to the first agent, and the default `max_turns`. The pages read do not say.
- Temporal's delivery and ordering guarantees for signals. The page read does not state them, so none are claimed here.
- A2A retry of push notifications. One section says agents may retry, another says they should.
- ADK's older description of three mechanisms (shared session state, model-driven transfer, `AgentTool`). The old URL redirects to the workflows pages, so `AgentTool` behaviour is not described here.
- What an AutoGen Core broadcast returns to the sender, and what happens to messages on a crash.
- Whether a running Claude Code subagent can message `main` and get an answer before it finishes. The roster lists `main` as a recipient, and the page does not describe that exchange.
- Exact wording on pages read through a summarising fetch: the Cognition posts, the two Anthropic articles on claude.com, and the AutoGen, OpenAI, Codex, CrewAI, Cursor, Devin, Copilot and Temporal child workflow pages. Their claims are paraphrased here. Text in quotation marks comes only from pages read raw (A2A, Claude Code agent teams and cross-session messaging).
- Whether Claude or Codex, as Reef runs them, accept a new message during a run. This decides if mid-run delivery of `continue` is possible and was not researched.
- How Reef's inbox code marks a message delivered. The suggestions in 6 (c) are written from `docs/design.md`, not from the code.

---

# Part 2: the tool set


Research for the delegation rework. Scope: the tool set only. The broad survey (communication
models, teams, reliability, UI) is in `delegation-research.md`. Sources were read on 2026-11-24;
names in these products change fast, so each source carries its version.

## Short answer (tools)

- Reef's four tools cover the four operations every source has: start, send more, stop, list. The
  set is right as far as it goes. Three operations are missing. Reading a worker's output on
  demand is in A2A, Claude Code and Codex v1. A worker state that says "I need input" is in A2A
  and MCP Tasks. Closing a worker is in Codex v1 and Claude Code teams.
- No source names the follow-up operation `continue`. A2A, Claude Code and Codex all call it
  "send message" (`SendMessage`, `SendMessage`, `send_message` / `send_input`). `delegate` has
  precedent (Goose's `delegate`, CrewAI's `Delegate work to coworker`). `stop_helper` matches the
  meaning of Claude Code's `TaskStop` and Codex's `interrupt_agent`: stop the run, keep the worker.
- The two protocols (A2A 1.0, MCP Tasks) do not give the worker an "ask" call that blocks. The
  worker's task moves to an `input-required` state and the caller answers with an ordinary
  message. Only CrewAI blocks in a tool call, and it runs every agent in one process. LangGraph
  pauses the graph at a checkpoint.
- A worker tool that blocks until the boss answers does not fit Reef: a waiting tool call emits no
  stream events, so the 15-minute idle limit would stop it, and a run going at a restart fails.
- Closing is common but not universal. Codex v1 has `close_agent`, Claude Code teams have a
  shutdown request, MCP Tasks has a time to live. A2A has no delete. Codex v2 registers no close
  tool, only `interrupt_agent`.
- Reading output on demand is standard in the protocols (`GetTask`, `tasks/get`). A blocking wait
  exists (`wait_agent`, A2A's blocking `SendMessage`) but both coding products steer away from it:
  Codex's `spawn_agent` text tells the model to use `wait_agent` rarely, and Claude Code delivers
  a background subagent's result as a notification in a later turn.
- Reef's model (start returns at once, the report wakes the boss) is the same model as Codex's
  completion notification, Claude Code's background subagents and A2A's
  `return_immediately` + push notification. It does not need to change.
- A helper thread maps onto an A2A `contextId` and a piece of helper work onto an A2A task. A2A
  refuses a message to a finished task, so "continue a finished helper" is a new task in the same
  context. The slash-style A2A names (`message/send`, `tasks/get`, `tasks/cancel`,
  `tasks/resubscribe`) are not in 1.0.1; the names are now `SendMessage`, `GetTask`, `CancelTask`,
  `SubscribeToTask`, plus `SendStreamingMessage` and `ListTasks`, and blocking is controlled by
  `return_immediately`.

## The operations

Each cell is the exact name in the source, or "none". Codex has two tool surfaces in the same
release; both are shown.

| Operation | Reef today | A2A 1.0.1 | Claude Code subagents | Claude Code agent teams | OpenAI Codex (0.172.0) | OpenAI Agents SDK | AutoGen / Magentic-One | LangGraph (supervisor, swarm) | CrewAI |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Start a worker | `delegate(bot, brief, repo?, read_only?)` | `SendMessage` (no `taskId`) | `Agent` (was `Task` before v2.1.63) | `Agent` with `name` | `spawn_agent` (v1: `message`, `items`, `agent_type`, `fork_context`; v2: `task_name`, `message`, `fork_turns`) | `transfer_to_<agent>` (handoff) or the tool made by `Agent.as_tool()` | ledger fields `next_speaker` + `instruction_or_question`, sent as `GroupChatRequestPublish` | `transfer_to_<agent_name>` | `Delegate work to coworker(task, context, coworker)` |
| Send it more / follow up | `continue(thread, message)` | `SendMessage` with `taskId` (live task) or `contextId` (new task, same context) | `SendMessage` (`to`: agent ID or name) | `SendMessage` | v1 `send_input(target, message, interrupt?)`; v2 `followup_task(target, message)` (starts a turn), `send_message(target, message)` (starts none) | none (call the tool again) | same as start | same as start | same as start |
| Answer its question | `continue` | `SendMessage` with the same `taskId` | `SendMessage` | `SendMessage` | same as follow up | none | same as start | `Command(resume=...)` | none (the answer is the tool result) |
| Worker asks parent / needs input | none (the question is the final reply) | task state `TASK_STATE_INPUT_REQUIRED` | none by default; `SendMessage` to its launcher if it has the tool; `AskUserQuestion` is removed from every subagent | `SendMessage` to the lead | v1 none; v2 `send_message` to the parent | none | none for the orchestrator; `UserInputRequestedEvent` for the person | `interrupt(value)` | `Ask question to coworker(question, context, coworker)` |
| Worker reports progress | none | `TaskStatusUpdateEvent`, `TaskArtifactUpdateEvent` | none | `SendMessage`, `TaskUpdate` | v1 none; v2 `send_message` | none | none (ledger field `is_progress_being_made`) | none | none |
| Worker reports result | final reply, delivered as a wake (`{Helper} finished thread {id}:`) | `TASK_STATE_COMPLETED` with artifacts, or a direct `Message` | final message as the `Agent` result or a completion notification; `SubagentHandback` in auto mode | idle notification (automatic, holds the final answer) | final-status notification holding the final message | tool output of the `as_tool` tool | `GroupChatAgentResponse` | `transfer_back_to_<supervisor>` (added by the library, not called by the worker) | return value of the tool |
| Wait / join | none | `SendMessage` blocks unless `return_immediately`; `SubscribeToTask` | none as a tool; a foreground `Agent` call blocks | none | `wait_agent` (v1: `targets`, `timeout_ms`; v2: `timeout_ms`) | the `as_tool` call blocks | turn-based, always blocks | graph step, always blocks | both tools block |
| Check status / list | `helpers()` | `GetTask`, `ListTasks` | none as a tool | `TaskList`, `TaskGet` (the shared task list, not the agents) | v1 none (status comes from `wait_agent`); v2 `list_agents(path_prefix?)` | none | none | none | none |
| Read output | none (the done wake holds the reply once) | `GetTask` with `history_length` | `Read` on the output file (`TaskOutput` removed in v2.1.277) | none as a tool | v1 `wait_agent` returns the final message; v2 none | none | none | none | none |
| Stop / cancel | `stop_helper(thread)` | `CancelTask` | `TaskStop` | `TaskStop` | v1 none (closest: `send_input` with `interrupt: true`, which redirects, and `close_agent`); v2 `interrupt_agent(target)` | none | none (closest: `GroupChatPause`, for the whole team) | none | none |
| Close / clean up | none (closing the boss thread closes its helpers) | none | none (transcripts deleted after `cleanupPeriodDays`, 30 days) | `SendMessage` with `shutdown_request`; team directories removed when the session ends | v1 `close_agent(target)`, `resume_agent(id)` reopens; v2 none | none | none (closest: `GroupChatReset`, which clears state) | none | none |
| Worker-to-worker message | none | none (any agent may be a client of another) | `SendMessage` between named subagents | `SendMessage` | v1 none; v2 `send_message` by canonical task name | `transfer_to_<agent>` between peers | shared group chat (`GroupChatMessage`) | swarm: `transfer_to_<agent_name>` | `Ask question to coworker` |

Notes on the table:

- Codex. Release `rust-v0.172.0` carries two surfaces. Feature `multi_agent` (namespace
  `multi_agent_v1`) is on by default and marked `Stage::Deprecated`. Feature `multi_agent_v2` is
  marked `Stage::Stable` and off by default. With v2 on, `spec_plan.rs` registers `spawn_agent`,
  `send_message`, `followup_task`, `wait_agent`, `interrupt_agent` and `list_agents`; otherwise
  the v1 five. Defaults: `agent_max_depth` 1, `agent_max_threads` 6 (v1), 4 concurrent threads
  per session (v2).
- Google ADK (2.15.0) is not a column. Its names: `sub_agents` on the parent,
  `transfer_to_agent(agent_name)` to hand over control, `AgentTool` (one argument, `request`) to
  call an agent and get its answer back in the same turn, `LongRunningFunctionTool` for a tool
  whose result arrives later. `get_user_choice` is the built-in long-running tool that asks the
  person; `exit_loop` sets `escalate`.
- Microsoft Agent Framework (the successor to AutoGen) uses one request/response mechanism for
  everything that needs an answer: a `request_info` event that the caller answers by `request_id`.
  Payloads: `HandoffAgentUserRequest`, `MagenticPlanReviewRequest` (answered with `approve()` or
  `revise(...)`).
- Goose (1.60.2): one tool, `delegate`. Its description says the result comes back once the
  delegate finishes. Its `resume` argument continues a failed, cancelled or completed delegate by
  subagent ID. Subagents cannot spawn subagents.
- LangGraph supervisor also has `forward_message`, a tool for the supervisor (not the worker) to
  pass a worker's message on unchanged.
- MCP Tasks (an extension, `io.modelcontextprotocol/tasks`): `tasks/get`, `tasks/update`,
  `tasks/cancel`; statuses `working`, `input_required`, `completed`, `failed`, `cancelled`.

## 1. Is this the right tool set?

What the sources do. Every source that runs workers in the background has the same core: start,
send a message, stop, and some way to see state. The two that are closest to Reef (Codex and
Claude Code, the providers Reef runs) add three things on top:

- A way to get the worker's output again. Codex v1 returns the final message from `wait_agent`;
  Claude Code writes a transcript and an output file that the parent reads with `Read`; A2A has
  `GetTask` with `history_length`.
- A way for the worker to reach its parent before it finishes. Codex v2's `spawn_agent` text
  says the new agent can message its parent and other running agents, and that its final answer
  is delivered when it finishes. Claude Code: a subagent with `SendMessage` can message its
  launcher.
- A way to end a worker's life (question 4).

What it suggests. Keep the four. Add a read. Add a needs-input state. Consider a close. Progress
reports and worker-to-worker messages exist in the team products only (Claude Code teams,
Codex v2); they are not part of the standard core and Reef's depth-1 design does not need them.

## 2. Are the names right?

What the sources do. See Naming below. No source uses `continue`. Three things Reef's `continue`
does have three different names elsewhere:

- answer or add work: "send message" (A2A `SendMessage`, Claude Code `SendMessage`, Codex
  `send_input`);
- start a new turn on an idle worker: Codex v2 `followup_task`;
- bring back a stopped or closed worker: "resume" (Codex v1 `resume_agent`, Goose `resume`,
  Claude Code's docs heading "Resume subagents", done through `SendMessage`).

Codex v2 split one tool into two because the difference matters there: `send_message` starts no
turn, `followup_task` does. Claude Code kept one tool: a message to a finished or stopped
subagent resumes it.

What it suggests. One tool is fine, as in Claude Code and A2A, because in Reef every message to a
helper starts or joins a run. On the name there is a trade-off:

- For `send_message`: it is the word A2A, Claude Code and Codex share, so a model has seen it
  with this meaning, and it maps onto A2A without a rename later.
- For `continue`: the A2A spec itself describes a follow-up as a way to "continue or refine" a
  task, and Reef's wake text already tells the boss to "Continue it". `design.md` records that
  the boss once used `continue` to ask about progress; it does not record why, and a tool called
  `send_message` could invite a status question just as easily. `helpers()` was the fix for that.

The standard word wins on "standard"; the rename is not needed for reliability. `delegate` and
`stop_helper` are fine. "Cancel" would be the wrong word for `stop_helper`: in A2A `CancelTask`
is terminal, and Reef's stop is not.

## 3. Should a helper get a tool to ask its boss?

What the sources do. Five shapes exist:

| Shape | Who | How the answer comes back |
| --- | --- | --- |
| A task state | A2A `TASK_STATE_INPUT_REQUIRED` ("an interrupted state"); MCP Tasks `input_required` | A2A: a new `SendMessage` with the same `taskId`. MCP: `tasks/update`. |
| Pause and checkpoint | LangGraph `interrupt(value)`; Agent Framework `request_info` | `Command(resume=...)`; a response keyed by `request_id`. LangGraph restarts the node from its first line on resume. |
| A message to a mailbox | Claude Code `SendMessage`; Codex v2 `send_message` | An ordinary message back. The worker does not block; it ends its turn or keeps working. |
| A blocking tool call | CrewAI `Ask question to coworker` | The tool's return value. Same process, synchronous. |
| Ending the turn with a question | Reef today; Claude Code subagents without `SendMessage`; OpenAI Agents SDK `as_tool` | The parent sends a follow-up. Nothing marks the reply as a question. |

No source that runs workers as separate long-lived sessions blocks the worker in a tool call.
Claude Code removes `AskUserQuestion` from every subagent.

What it suggests. Give the helper one tool that records "I need input" and returns at once, the
A2A shape. The helper then ends its turn; the boss is woken with a notice that says it is a
question, not a finish; the boss answers with the ordinary message tool. The ask notice must
replace the `finished` wake for that run, not come in addition: one pending item, never twice.
This fits Reef's constraints: nothing waits inside a tool call, so the idle limit and a restart
cannot break it, and the boss is never blocked. It also gives `helpers()` a true state
(`needs_input`) in place of `done` for a helper that is in fact waiting. Whether models call the
tool reliably, or still end with a question in prose, needs a spike like the `tell_user` one; the
prose path must keep working either way.

## 4. Should the boss be able to close a helper?

What the sources do.

- Codex v1: `close_agent`. Its text says a completed agent stays open and counts toward the
  concurrency limit until it is closed. `resume_agent` reopens a closed agent.
- Codex v2: no close tool is registered. `interrupt_agent` stops the turn and leaves the agent
  available for messages and follow-up tasks. `list_agents` lists live agents. The Codex docs say
  the person can ask Codex to close completed subagent threads; how v2 does that was not traced.
- Claude Code teams: the lead sends a shutdown request, and the teammate approves and exits or
  rejects with a reason. Cleanup of the team's directories is automatic when the session ends;
  the docs say there is no separate cleanup step.
- Claude Code subagents: no close. Transcripts are deleted after `cleanupPeriodDays` (30 days).
- A2A: no delete operation. Terminal states are final; expiry and cleanup of a context are left
  to each agent.
- MCP Tasks: a time to live (`ttlMs`) on each task.

What it suggests. Closing is a local lifecycle matter, not a protocol one; every source handles
it differently. Reef's limit counts running runs, not open threads, so a close is not needed to
free a slot (the reason Codex v1 has it). It is needed for two smaller things: `helpers()` lists
every open helper thread for the life of the boss thread, and a helper on its own branch keeps
its worktree. A `close_helper` that does what the person's Close does is the smallest fix. The
shutdown handshake (request, approve or reject) is more than Reef needs: a Reef close already
saves unfinished work.

## 5. Read output on demand, or wait?

What the sources do.

- Read: A2A `GetTask` (with `history_length`), MCP `tasks/get`, Codex v1 `wait_agent` (returns the
  final message), Claude Code `Read` on the subagent's output file. Claude Code removed its
  dedicated `TaskOutput` tool in v2.1.277.
- Wait: Codex `wait_agent` blocks up to `timeout_ms` (default 30 s, minimum 10 s, maximum 1 h).
  v1 returns final statuses, with the final message. v2 returns no content, only which agents
  have updates, and ends early when the person types. A2A `SendMessage` blocks by default and
  `return_immediately: true` turns that off. Claude Code has no wait tool; a foreground `Agent`
  call blocks, a background one reports later.
- Codex's `spawn_agent` text tells the model to call `wait_agent` only when the next step on the
  critical path needs the result at once, and not to wait by reflex.
- Codex's `wait_agent` text also says the notification comes anyway: once an agent reaches a
  final status, the parent gets a message with the same status.

What it suggests. Add a read; do not add a wait. The read closes a real gap: the done wake holds
the helper's reply once, and after that the boss has no way back to it (a long boss thread, a
second look before merging, a stuck or stopped helper whose last reply was never delivered). A
wait buys nothing the wake does not already give, and a boss run parked in a tool call is exactly
what the idle limit stops.

## Naming

| Operation | Names seen | Most common word |
| --- | --- | --- |
| Start a worker | `delegate` (Reef, Goose), `Delegate work to coworker` (CrewAI), `spawn_agent` (Codex), `Agent` (Claude Code), `SendMessage` (A2A), `transfer_to_<agent>` (OpenAI Agents SDK, LangGraph, ADK `transfer_to_agent`) | spawn / delegate for "work for me and report back"; transfer / handoff for "take over the conversation" |
| Follow up | `SendMessage` (A2A, Claude Code), `send_message`, `send_input`, `followup_task` (Codex), `continue` (Reef), `resume` argument (Goose) | send message |
| Worker needs input | `TASK_STATE_INPUT_REQUIRED` (A2A), `input_required` (MCP Tasks), `interrupt` (LangGraph), `request_info` (Agent Framework), `Ask question to coworker` (CrewAI) | input required |
| Result | final message / final answer (Claude Code, Codex), `completed` (A2A, MCP Tasks, Codex status), idle notification (Claude Code teams) | completed, final message |
| Wait | `wait_agent` (Codex), blocking `SendMessage`, `SubscribeToTask` (A2A) | wait |
| Status / list | `list_agents` (Codex v2), `ListTasks`, `GetTask` (A2A), `tasks/get` (MCP Tasks), `helpers` (Reef) | list, get |
| Read output | `GetTask` (A2A), `tasks/get` (MCP Tasks), `Read` on a file (Claude Code) | get |
| Stop | `TaskStop` (Claude Code), `interrupt_agent` (Codex v2), `CancelTask` (A2A), `tasks/cancel` (MCP Tasks), `stop_helper` (Reef) | stop / interrupt when the worker lives on; cancel when it is terminal |
| Close | `close_agent` (Codex v1), `shutdown_request` (Claude Code teams) | close, shut down |
| Worker states | A2A: submitted, working, input-required, auth-required, completed, failed, canceled, rejected. Codex: `pending_init`, `running`, `interrupted`, `completed`, `errored`, `shutdown`, `not_found`. MCP Tasks: working, input_required, completed, failed, cancelled. Reef: `running`, `done`, `failed`, `stopped`, `limited`, `stuck` | working / running, completed, failed, canceled |

## Suggested tool set for Reef

This section is a suggestion, not a finding. It is the smallest set that covers the operations
the sources agree on and keeps Reef's rules: the boss is never blocked on a helper, reports wake
the boss, depth 1, the person can always stop.

| Tool | Who | Parameters | Returns | Blocks | Follows |
| --- | --- | --- | --- | --- | --- |
| `delegate` (keep) | boss | `bot`, `brief`, `repo?`, `read_only?` | `{thread_id, branch?, note?, helpers}` | no | Codex `spawn_agent`, Goose `delegate`, A2A `SendMessage` with `return_immediately` |
| `send_message` (rename of `continue`) | boss | `thread`, `message` | `Sent.` or `Queued: {Helper} is working; it gets this when its run ends.`, then the helpers line | no | A2A `SendMessage`, Claude Code `SendMessage`, Codex `send_input` |
| `stop_helper` (keep) | boss | `thread` | `Stopped {Helper}.` and the helpers line | no (returns when the run has stopped, within 5 s) | Claude Code `TaskStop`, Codex `interrupt_agent` |
| `helpers` (keep, one new state) | boss | none | `{helpers: [...], line}`; `state` gains `needs_input` | no | Codex `list_agents`, A2A `ListTasks` |
| `read_helper` (new) | boss | `thread` | the helper's last reply, its state, and the question when the state is `needs_input`. Read-only: starts no run, writes nothing | no | A2A `GetTask`, MCP `tasks/get`, Codex v1 `wait_agent`'s result without the wait |
| `close_helper` (new) | boss | `thread` | `Closed {Helper}.` and the helpers line. Stops a going run, saves unfinished work as Close does, drops the thread from `helpers()` | no | Codex v1 `close_agent`, Claude Code teams shutdown |
| `ask_boss` (new) | helper | `question` | `Asked {Boss}. End your turn; the answer comes as your next message.` Sets the thread to `needs_input`. When the run ends, the boss's wake is `{Helper} asks in thread {id}: {question}` in place of the `finished` wake | no | A2A `TASK_STATE_INPUT_REQUIRED`, MCP Tasks `input_required` |

Left out on purpose:

- A blocking wait. The wake already delivers the result; a parked boss run would hit the idle
  limit. Codex tells its own model to use `wait_agent` rarely.
- A progress tool for the helper. Only the team products have one. `helpers()` already shows idle
  time, branch and commits ahead without a wake.
- Worker-to-worker messages. Depth 1 and "a helper's replies go to its boss only" rule them out.
- A separate "resume". As in Claude Code, a message to a stopped, stuck or finished helper resumes
  it.

Order of value, if this is done in steps: `read_helper` (pure addition, no behaviour change),
then `ask_boss` (needs a spike), then `close_helper`. The rename is the least urgent: it buys the
standard word and the A2A mapping, not reliability (question 2), and it touches the locked e2e
tests and every prompt that names `continue`.

What maps 1:1 onto A2A later:

| Reef | A2A 1.0 |
| --- | --- |
| helper thread | `contextId` |
| one helper run | one `Task`. Exception: a run that ends in `needs_input` and the run that gets the answer are the same `Task` |
| `delegate` | `SendMessage` with no `taskId`, `return_immediately: true` |
| `send_message` to a helper that is running or in `needs_input` | `SendMessage` with that `taskId` |
| `send_message` to a helper whose run has ended | `SendMessage` with the `contextId` only: a new task in the same context (A2A refuses a message to a terminal task) |
| `stop_helper` | `CancelTask` |
| `helpers` | `ListTasks` (one call per helper `context_id`, or one unfiltered call, since the filter takes a single context) |
| `read_helper` | `GetTask` with `history_length` |
| `ask_boss` | the task enters `TASK_STATE_INPUT_REQUIRED` |
| the wake | a push notification (`CreateTaskPushNotificationConfig`) or `SubscribeToTask` |
| states `running`, `done`, `failed`, `stopped`, `needs_input` | `TASK_STATE_WORKING`, `_COMPLETED`, `_FAILED`, `_CANCELED`, `_INPUT_REQUIRED` |
| a `delegate` refusal | `TASK_STATE_REJECTED` |

What does not map: `close_helper` (A2A has no delete), `limited` and `stuck` (both would be
`TASK_STATE_FAILED` with a message), and a stopped helper that is later continued (in A2A the
canceled task stays canceled and the continuation is a new task).

## Sources (part 2)

All accessed 2026-11-24.

| URL | Version | What was taken |
| --- | --- | --- |
| https://github.com/a2aproject/A2A/blob/v1.0.1/specification/a2a.proto | v1.0.1 (2026-05-28) | RPC names, `TaskState` values and comments, `SendMessageConfiguration.return_immediately`, `ListTasksRequest`, `GetTaskRequest.history_length` |
| https://github.com/a2aproject/A2A/blob/v1.0.1/docs/specification.md | v1.0.1 | Method mapping table (section 5.3), blocking and non-blocking rule, terminal-state rule, `contextId` and `taskId` rules (section 3.4), `CancelTask` errors |
| https://github.com/a2aproject/A2A/releases | - | Release list: v1.0.1, v1.0.0 (2026-03-12), v0.3.0 (2025-07-30) |
| https://github.com/openai/codex/blob/rust-v0.172.0/codex-rs/core/src/tools/handlers/multi_agents_spec.rs | rust-v0.172.0 (2026-11-24) | Every Codex tool name, parameter, description and output schema quoted here |
| https://github.com/openai/codex/blob/rust-v0.172.0/codex-rs/core/src/tools/handlers/multi_agents_v2.rs | rust-v0.172.0 | The v2 handler list (no close handler) |
| https://github.com/openai/codex/blob/rust-v0.172.0/codex-rs/core/src/tools/spec_plan.rs | rust-v0.172.0 | Which tools are registered with `multi_agent_v2` on and off |
| https://github.com/openai/codex/blob/rust-v0.172.0/codex-rs/features/src/lib.rs | rust-v0.172.0 | Feature keys `multi_agent` (Deprecated, default on) and `multi_agent_v2` (Stable, default off) |
| https://github.com/openai/codex/blob/rust-v0.172.0/codex-rs/core/src/config/mod.rs | rust-v0.172.0 | Wait timeouts, `DEFAULT_AGENT_MAX_DEPTH`, `DEFAULT_AGENT_MAX_THREADS`, v2 concurrency |
| https://github.com/openai/codex/blob/rust-v0.172.0/codex-rs/app-server-protocol/schema/typescript/v2/CollabAgentStatus.ts | rust-v0.172.0 | Agent status values |
| https://developers.openai.com/codex/subagents (redirects to https://learn.chatgpt.com/docs/agent-configuration/subagents) | undated | `agents.enabled` default, how the person steers, stops and closes subagents. The page names no tools |
| https://code.claude.com/docs/en/tools-reference | undated; cites versions up to v2.1.317 | `Agent`, `SendMessage`, `TaskStop`, `TaskOutput` (removed), `TaskCreate` / `TaskGet` / `TaskList` / `TaskUpdate`, `SubagentHandback` |
| https://code.claude.com/docs/en/sub-agents (and the raw `sub-agents.md`) | undated | `Task` renamed `Agent` in v2.1.63, foreground and background, resume through `SendMessage`, the tools removed from every subagent (`AskUserQuestion` among them), depth limit, transcripts and `cleanupPeriodDays` |
| https://code.claude.com/docs/en/agent-teams | undated | Teammates through `Agent` with `name`, mailbox, idle notification, shutdown request, automatic cleanup, "send one message per recipient", limitations |
| https://code.claude.com/docs/en/agent-sdk/subagents | undated | `AgentDefinition` fields, `status: "async_launched"` and `"completed"`, `agentId`, depth and concurrency defaults |
| https://github.com/google/adk-python/tree/v2.15.0/src/google/adk/tools | v2.15.0 (2026-11-19) | `transfer_to_agent_tool.py`, `agent_tool.py`, `long_running_tool.py`, `get_user_choice_tool.py`, `exit_loop_tool.py`; `sub_agents` from `agents/base_agent.py` |
| https://github.com/openai/openai-agents-python/blob/main/src/agents/handoffs/__init__.py | main; latest release v0.26.0 (2026-11-24) | Default handoff tool name `transfer_to_<agent>` and description |
| https://github.com/openai/openai-agents-python/blob/main/src/agents/agent.py | main | `Agent.as_tool()` signature and docstring |
| https://github.com/microsoft/autogen/tree/python-v0.7.5/python/packages/autogen-agentchat/src/autogen_agentchat | python-v0.7.5 (2025-09-30) | `teams/_group_chat/_events.py` message classes, `_magentic_one/_prompts.py` ledger fields, `messages.py` `HandoffMessage` and `UserInputRequestedEvent`, `tools/_agent.py` `AgentTool` |
| https://learn.microsoft.com/en-us/agent-framework/workflows/orchestrations/magentic | updated 2026-10-06 | `MagenticBuilder` limits, `MagenticPlanReviewRequest` / `Response`, orchestrator events |
| https://learn.microsoft.com/en-us/agent-framework/workflows/orchestrations/handoff | updated 2026-10-15 | `request_info`, `HandoffAgentUserRequest`, autonomous mode |
| https://github.com/langchain-ai/langgraph-supervisor-py/blob/main/langgraph_supervisor/handoff.py | main; latest release 0.0.31 (2025-11-19) | `create_handoff_tool`, `transfer_to_<agent_name>`, `transfer_back_to_<supervisor>`, `forward_message`, `Command(goto=..., graph=Command.PARENT)` |
| https://github.com/langchain-ai/langgraph-swarm-py/blob/main/langgraph_swarm/handoff.py | main; latest release 0.1.0 (2025-12-04) | `transfer_to_<agent_name>` |
| https://docs.langchain.com/oss/python/langgraph/interrupts | undated | `interrupt()`, `Command(resume=...)`, checkpointer and thread ID needed, node restarts on resume |
| https://github.com/crewAIInc/crewAI/tree/1.15.45/lib/crewai/src/crewai/tools/agent_tools | 1.15.45 (2026-11-24) | `delegate_work_tool.py`, `ask_question_tool.py`, `base_agent_tools.py` (synchronous `execute_task`), and `translations/en.json` for the descriptions |
| https://github.com/aaif-goose/goose/blob/v1.60.2/crates/goose/src/agents/platform_extensions/summon.rs | v1.60.2 (2026-11-20) | `delegate` tool, `DelegateParams`, `resume` |
| https://github.com/aaif-goose/goose/blob/v1.60.2/documentation/docs/guides/context-engineering/subagents.mdx | v1.60.2 | Subagents cannot spawn subagents |
| https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/extensions/tasks/overview.mdx | main | Tasks extension: `tasks/get`, `tasks/update`, `tasks/cancel`, statuses, `ttlMs` |
| https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/basic/utilities/tasks.mdx | 2025-11-25 | The earlier, experimental form (`tasks/result`, `tasks/list`) |

## Not confirmed (part 2)

- Claude Code agent teams: no `TeamCreate`, `TeamDelete`, broadcast or separate cleanup tool is in
  the current tools reference or teams page. Older write-ups name them; the current pages say
  "send one message per recipient" and "there's no separate cleanup step". Whether they existed
  and were removed was not checked.
- Claude Code: the exact input schema of `Agent`, `SendMessage` and `TaskStop` (parameter lists)
  is not on the pages read. Parameters named in prose: `subagent_type`, `name`, `model`, `effort`,
  `isolation`, `run_in_background` for `Agent`; `to`, `summary` for `SendMessage`. The shape of a
  `shutdown_request` message and of the teammate's approve or reject reply is not documented.
- Claude Code: whether a message from Claude to a subagent that is still running is queued or
  interrupts it. The sub-agents page covers finished and stopped subagents; a search of the whole
  page found no statement for a running one.
- Codex: which surface a given model gets in practice. The flags say v1 is on by default and v2
  off, but v1 is marked deprecated and v2 stable. How a v2 agent's life ends without a close tool
  was not traced in the source.
- The A2A method names of 0.3 and earlier (`message/send`, `tasks/resubscribe`, `blocking`) come
  from the brief and memory; the v0.3.0 tag was not read. Only their absence from 1.0.1 was
  checked.
- Goose: that `delegate` blocks the caller is read from its description ("result back once it
  finishes"), not from the handler code.
- Codex: the public docs page names no tools, so every Codex tool name here rests on the source at
  one release tag.
- OpenAI Agents SDK, LangGraph supervisor and swarm: read from `main`, not from a release tag.
- Microsoft Agent Framework: the GitHub repo could not be reached; names come from the Microsoft
  Learn pages only. The exact handoff tool name pattern is described there in prose, not quoted.
- AutoGen: `python-v0.7.5` is the latest release found; the project has since moved to Agent
  Framework. The table's AutoGen cells are internal message classes, not model-facing tools:
  Magentic-One's orchestrator picks the next speaker in a JSON ledger, it calls no tool.
- MCP Tasks: read from the repo's `main` branch. The tasks page is absent from the 2026-07-28
  spec folder and present under `docs/extensions`, so it is treated here as an extension; the
  `ext-tasks` repository that holds the normative text was not read.
- Devin, Cursor, Amp and opencode: not covered. opencode's source shows a subagent tool input with
  `agent`, `subagent_type`, `description`, `sessionID`, `model`; the tool's name and behaviour
  were not confirmed.
- The claim that a waiting tool call counts toward Reef's idle limit rests on `docs/design.md`
  ("seconds since the run's last stream event of any kind"); it was not run.
