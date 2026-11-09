# Owner's taste and product principles (living)

What the owner wants Reef to be, distilled from their remarks. The driver adds to it every time the owner reacts to
something: write the principle behind the remark, not the remark. Observers judge with it; the lead and coders get it
in their briefs. Each line: the principle, then (where it came from).

## Product
- Reef should keep getting better as a product, not just match its spec. The spec is the current answer; propose a
  better one with evidence. (round 1: "critique, find and improve as product evolves")
- Conversation first. A person answers in plain words, anywhere. No UI should force a specific widget or block the
  composer; replying in chat should resolve whatever was waiting. (approval card: "if user replied from compose it
  supposed to be gone")
- People must always see what needs them, once, everywhere it matters (thread, sidebar, island), and nothing else
  should compete for that attention. (decisions missed for 45 min; "Waiting on you" card)
- Parallel work is the normal case: progress, ownership and spend across helpers must be readable at a glance.
- Bots and people talk like humans: short, plain, specific. Long reports are a smell; the answer comes first.
- Trust comes from evidence: show what ran and what it cost; never claim what wasn't checked.
- Privacy and isolation are not negotiable: a Bot sees only what it was given.

## Look and feel
- Calm and compact. Small, dense where it helps reading, no wasted height. (approval card "bit compact")
- Colour carries meaning only; red is rare and reserved for real danger or failure, never a resting state.
  ("no need to put that much red")
- Our own components, built for Reef, richer and more adaptable than off-the-shelf; no generic renderers like
  Mermaid. ("we write our own components richer and adaptable to our product")
- Show data visually when it helps: tables, charts, diffs, code, metrics, timelines, as Reef components; sandboxed
  HTML only for what the set can't do. (rich output idea; T3 Code's HTML embedding)
- Motion supports state, never decorates; reduced motion is respected.
- Errors are designed messages: plain words, what to do next, an action, details tucked away; never a raw object,
  JSON, error chain or "internal error" on screen. ("not supposed to render object as error message")

## How we build
- Smallest change that closes the ticket, test first from a separate session, owner merges.
- Fix what was found, all of it; group small related findings into one PR.
- Prefer one clear design over options; when a decision is needed, ask once, with a recommendation.
- Cost and context are product features: cheaper models where quality holds, caching, no waste.

## How to apply (observers and Bots)
- Generalise: when the owner reacts to one instance, find every instance of the same problem and fix the class.
  ("less red" means audit every colour of alarm, not only the approval card.)
- Critique the design, not only the bug: ask whether this is the right thing at all, then what the smallest better
  step is.
- Propose concretely: today's screenshot or numbers, the problem in one sentence, the change, the payoff, how to test
  it; a mockup when words aren't enough. Rank by user payoff.
- Track evolution: each round, re-check last round's findings and fixes. Was it fixed? Is the fix good? Did it
  regress something? New features get the hardest look.
- When unsure what the owner would want, apply these principles and say which one you used.

## Learned while driving (round 1)
- Fix the class: a security hole found for one provider (Codex sandbox) usually exists for the others (Claude Auto has
  no sandbox); harden the shared path (Reef's own git calls), not just the provider. (#122)
