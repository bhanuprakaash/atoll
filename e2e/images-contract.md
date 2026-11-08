# Images test contract

Extends [mac-app-contract.md](mac-app-contract.md) for issue #89. The real binary is driven over
HTTP by Go tests and the browser UI by Playwright; both use the existing scripted fake providers.
JSON is snake_case, IDs are strings, and replies may include fields not listed here.

## REST

| Call                                  | Body                             | Reply                                                                   |
| ------------------------------------- | -------------------------------- | ----------------------------------------------------------------------- |
| `POST /api/threads/{id}/messages`     | `{text, images: [{mime, data}]}` | as before; the message has `attachments: [{id, mime}]`                  |
| `POST /api/bots/{id}/threads`         | `{text, images: [{mime, data}]}` | as before; the first message has `attachments`                          |
| `GET /api/attachments/{id}`           | none                             | `200` the exact bytes, `Content-Type` the sent mime; `404` when unknown |
| `GET /api/threads/{id}/file?path=<p>` | none                             | `200` the image file, `Content-Type` by extension; `404` otherwise      |

`data` is base64. Images with empty text are accepted; empty text and no images is `400`.

## Limits

Allowed mimes: `image/png`, `image/jpeg`, `image/gif`, `image/webp`. Each image is at most 5 MB
decoded; a message has at most 10. Any breach is `400` and stores nothing: no message, no run.

## Storage

`GET /api/threads/{id}/messages` lists each message's `attachments`; they survive a server
restart. Deleting the Bot deletes its attachments: `GET /api/attachments/{id}` becomes `404`.

## Providers

- Claude gets, in the stream-json user message, content blocks: the text block first, then for each
  image `{"type":"image","source":{"type":"base64","media_type":<mime>,"data":<base64>}}`.
- Codex gets, in `turn/start` input: the text item first, then each image as
  `{"type":"image","url":"data:<mime>;base64,<data>"}`.

## Thread files

`GET /api/threads/{id}/file?path=<p>` serves an image file (extension `png`, `jpg`, `jpeg`, `gif`
or `webp`) inside the thread's worktree, with its image `Content-Type`. A relative `p` resolves
against the worktree. A path outside the worktree (`../`, an absolute path, a symlink out), a
non-image file or a missing file is `404`.

## UI contract

- Pasting an image into the composer (a `paste` event with a `DataTransfer` holding a `File`)
  shows a thumbnail, `data-testid="composer-attachment"`, above the text box, with a button named
  `Remove image`. Dropping a file on the composer shows a thumbnail too.
- A file over 5 MB or a non-image file shows a message (`role="alert"`) and adds no thumbnail.
- After send, the user message shows `data-testid="message-attachment"` img elements; they remain
  after reload.
- Clicking one opens a `role="dialog"` named `Image` with the full-size img. Escape closes it; a
  click on the backdrop closes it.
- A reply containing `![](<relative path in the worktree>)` renders an img whose src loads
  (`naturalWidth > 0`) and that opens the same dialog.

## Fixtures

`testdata/claude/images-reply.jsonl` answers "Show me the screenshot at shot.png." with
`Here it is: ![](shot.png)`; the UI test commits `shot.png` into its repo.

## Acceptance mapping

| Acceptance                                  | Tests                           |
| ------------------------------------------- | ------------------------------- |
| Send, store, serve, restart                 | `TestImages_SendStoreAndServe`  |
| Limits and empty input                      | `TestImages_Rejected`           |
| Provider input                              | `TestImages_ProviderInput`      |
| Bot deletion                                | `TestImages_DeleteBot`          |
| Thread files                                | `TestImages_ThreadFile`         |
| Composer paste, drop, remove, invalid files | `images.spec.ts` composer tests |
| Message images, lightbox, reload            | `images.spec.ts` message test   |
| Reply image                                 | `images.spec.ts` reply test     |
