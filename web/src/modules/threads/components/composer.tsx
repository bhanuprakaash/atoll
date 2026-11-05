import { useLayoutEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { TextArea } from '@/components/text-field';
import { useSendMessage, type Run } from '@/modules/threads/api/messages';
import { useStopRun } from '@/modules/threads/api/runs';

const roundButton =
  'flex size-10 flex-none items-center justify-center rounded-full bg-accent text-accent-ink disabled:bg-segment disabled:text-muted';

export function Composer({
  threadId,
  botName,
  runId,
  disabled,
  onSent,
  context,
  onFirstSend,
}: {
  threadId: string;
  botName: string;
  runId?: string;
  disabled: boolean;
  onSent: (run: Run) => void;
  context?: ReactNode;
  onFirstSend?: (text: string) => void;
}) {
  const [text, setText] = useState('');
  const box = useRef<HTMLTextAreaElement>(null);
  const send = useSendMessage(threadId);
  const stop = useStopRun();
  const canSend = !disabled && !send.isPending && text.trim() !== '';

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, [text]);

  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    if (!canSend) return;
    if (onFirstSend) {
      onFirstSend(text);
      setText('');
      return;
    }
    send.mutate(text, {
      onSuccess: ({ run }) => {
        setText('');
        onSent(run);
      },
    });
  };

  const input = (
    <TextArea
      ref={box}
      variant="bare"
      aria-label={`Message ${botName}${runId ? ', run in progress' : ''}`}
      placeholder={
        runId
          ? `${botName} is working…`
          : onFirstSend
            ? `Ask ${botName} anything, or paste an issue or MR link`
            : `Message ${botName}`
      }
      rows={1}
      value={text}
      onChange={(e) => setText(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
          e.preventDefault();
          submit();
        }
      }}
      className="max-h-50 py-2 text-[15px] leading-normal"
    />
  );
  const action = runId ? (
    <button
      type="button"
      aria-label="Stop"
      disabled={stop.isPending}
      onClick={() => stop.mutate(runId)}
      className={roundButton}
    >
      <svg aria-hidden="true" width="14" height="14" viewBox="0 0 14 14">
        <rect x="2" y="2" width="10" height="10" rx="2" fill="currentColor" />
      </svg>
    </button>
  ) : (
    <button type="submit" aria-label="Send" disabled={!canSend} className={roundButton}>
      <svg
        aria-hidden="true"
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M12 19V5M5 12l7-7 7 7" />
      </svg>
    </button>
  );

  return (
    <div className="flex flex-col gap-2">
      <form
        onSubmit={submit}
        className={
          context
            ? 'flex flex-col gap-1.5 rounded-[26px] border border-composer-line bg-composer pt-2 pr-2.5 pb-2.5 pl-4.5'
            : `flex items-end gap-2.5 border border-composer-line bg-composer py-1.5 pr-2 pl-4.5 ${onFirstSend ? 'min-h-15 rounded-[30px]' : 'min-h-14 rounded-[28px]'}`
        }
      >
        {context ? (
          <>
            <div className="pr-2">{input}</div>
            <div className="flex items-center gap-1.5">
              <div
                role="group"
                aria-label="Thread context"
                className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5 text-[12.5px] text-ink/80"
              >
                {context}
              </div>
              {action}
            </div>
          </>
        ) : (
          <>
            <div className="min-w-0 flex-1 self-center">{input}</div>
            {action}
          </>
        )}
      </form>
      {(send.isError || stop.isError) && (
        <p className="text-[13px] text-danger">{send.error?.message || stop.error?.message}</p>
      )}
    </div>
  );
}
