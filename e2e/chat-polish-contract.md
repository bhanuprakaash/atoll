# Chat polish test contract

Extends [mac-app-contract.md](mac-app-contract.md) for issue #21. The real binary is driven
over HTTP by Go tests and the browser UI by Playwright; both use the existing scripted fake
providers. JSON is snake_case, IDs are strings, and replies may include fields not listed here.

## REST additions

| Call | Body | Reply |
| --- | --- | --- |
| `PATCH /api/threads/{id}` | `{title}` | `200` the thread |
| `POST /api/bots/{id}/pin` | none | `204` |
| `DELETE /api/bots/{id}/pin` | none | `204` |
| `GET /api/threads/{id}/messages?before={message id}` | none | `200` older messages |

`GET /api/bots` and `GET /api/bots/{id}` include `pinned` (a boolean). Pinning Chief returns
`409`. Pins persist through a server restart. The response order is Chief, then pinned Bots in
the order pinned, then unpinned Bots in their existing recent order.

## Threads and messages

A new thread's title is empty. When its first message is sent, its title becomes the first six
whitespace-separated words of that message, with trailing `.`, `,`, `;`, `:`, `!`, or `?` removed
from the sixth word. Fewer words remain unchanged except for that trailing punctuation. A title
set by `PATCH` is a user title and sending the first message must not replace it.

`GET /api/threads/{id}/messages` returns the newest 50 messages, in chronological order.
`before` is a message ID from that thread and returns up to the preceding 50 messages, also in
chronological order. A message has its stable `id`, role, text and created time.

## UI contract

- The new title appears in the thread header and the Threads panel. The sidebar presently has no
  thread-title surface, so it has no title assertion.
- A sidebar Bot row's second line exactly matches its API `activity`: the live run verb while it
  works, `Needs you: …` for an approval, otherwise the latest reply's first line, at most 60
  characters at a word boundary, followed by `…` only when cut. It never says `Done`.
- Each user message and reply has a `data-testid="message-time"` beneath it. It displays `H:MM`
  today, `Yesterday H:MM` yesterday, `Mon H:MM` within the last seven days, otherwise
  `6 Nov H:MM`. It is 12px and has the same computed color as an existing muted element.
- Each message's outer visual block has `data-testid="message"`; the user block retains its
  existing `article` role and `You` accessible name.
- The gap from a user message to its reply, and from that reply to the next user message, is at
  least 12px. The fake recordings have no multi-line reply, so this contract has no
  intra-reply-line comparison.
- Hovering a sidebar row exposes `Pin <Bot name>` or `Unpin <Bot name>`. Bot settings exposes
  `Pin to sidebar`.
- The message log first renders the latest 50. Reaching its top loads the preceding page without
  moving the former top message by more than 2px.
- When scrolled away from its end, a round `Jump to latest` button appears above the composer.
  It reaches the last message when clicked, is absent at the bottom, and during a run has
  `data-working="true"` with the working creature inside.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Auto title and user title | `TestChatPolish_AutoTitleAndUserTitle`, `chat-polish.spec.ts` title test |
| Sidebar activity | `TestChatPolish_SidebarActivity`, `chat-polish.spec.ts` activity test |
| Message time | `chat-polish.spec.ts` message time test |
| Turn spacing | `chat-polish.spec.ts` turn spacing test |
| Pin Bots | `TestChatPolish_PinBotsPersistAndOrder`, `chat-polish.spec.ts` pin controls test |
| Older messages | `TestChatPolish_OlderMessages`, `chat-polish.spec.ts` older messages test |
| Jump to latest | `chat-polish.spec.ts` jump test |
