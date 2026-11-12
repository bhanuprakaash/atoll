import { useState } from 'react';
import { quiet } from '@/components/button';
import { TextArea } from '@/components/text-field';
import { ZoomImage } from '@/components/zoom-image';
import { useChangeQueued, type Message } from '@/modules/threads/api/messages';

const chatText = 'text-base leading-[1.65] tracking-normal';

// QueuedMessage is the user's bubble for a message that waits for the run to end.
export function QueuedMessage({
  message,
  threadId,
  running,
  editable,
}: {
  message: Message;
  threadId: string;
  running: boolean;
  editable: boolean;
}) {
  const change = useChangeQueued(threadId);
  const [draft, setDraft] = useState<string | null>(null);
  return (
    <div data-testid="message" className="flex max-w-[75%] flex-col items-end gap-2 self-end">
      <article aria-label="You" className="flex flex-col items-end gap-1.5">
        {message.attachments.length > 0 && (
          <div className="flex flex-wrap justify-end gap-1.5">
            {message.attachments.map((a) => (
              <ZoomImage
                key={a.id}
                src={`/api/attachments/${a.id}`}
                alt="Attachment"
                testId="message-attachment"
                className="max-h-40 max-w-60 rounded-2xl object-cover"
              />
            ))}
          </div>
        )}
        {draft === null ? (
          message.text && (
            <div
              className={`rounded-[22px] bg-accent px-[18px] py-2.5 ${chatText} whitespace-pre-wrap text-accent-ink [overflow-wrap:anywhere]`}
            >
              {message.text}
            </div>
          )
        ) : (
          <div className="flex w-full min-w-80 flex-col items-end gap-2">
            <TextArea
              aria-label="Edit message"
              rows={2}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              className="text-[15px]"
            />
            <div className="flex gap-3">
              <button type="button" className={quiet} onClick={() => setDraft(null)}>
                Cancel
              </button>
              <button
                type="button"
                className={quiet}
                disabled={change.isPending}
                onClick={() =>
                  change.mutate(
                    { id: message.id, text: draft },
                    { onSuccess: () => setDraft(null) },
                  )
                }
              >
                Save
              </button>
            </div>
          </div>
        )}
        <div className="flex flex-wrap items-center justify-end gap-3 pr-[18px]">
          <span className="text-xs text-muted">Queued</span>
          {editable && draft === null && (
            <>
              <button type="button" className={quiet} onClick={() => setDraft(message.text)}>
                Edit
              </button>
              <button
                type="button"
                className={quiet}
                disabled={change.isPending}
                onClick={() => change.mutate({ id: message.id })}
              >
                Remove
              </button>
              <button
                type="button"
                className={quiet}
                disabled={change.isPending}
                onClick={() => change.mutate({ id: message.id, send: true })}
              >
                {running ? 'Stop and send' : 'Send now'}
              </button>
            </>
          )}
        </div>
      </article>
    </div>
  );
}
