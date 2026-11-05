import { useState, type FormEvent, type ReactNode } from 'react';
import { Button } from '@/components/button';
import { useSendMessage, type Run } from '@/modules/threads/api/messages';
import { useStopRun } from '@/modules/threads/api/runs';

export function Composer({
  threadId,
  runId,
  disabled,
  onSent,
  context,
  onFirstSend,
}: {
  threadId: string;
  runId?: string;
  disabled: boolean;
  onSent: (run: Run) => void;
  context?: ReactNode;
  onFirstSend?: (text: string) => void;
}) {
  const [text, setText] = useState('');
  const send = useSendMessage(threadId);
  const stop = useStopRun();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!text.trim()) return;
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
  return (
    <form
      onSubmit={submit}
      className="flex flex-col gap-2 rounded-card border border-line bg-composer p-4"
    >
      <textarea
        aria-label="Message"
        className="w-full resize-none bg-transparent outline-none"
        rows={3}
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="flex items-center gap-2">
        <div
          role="group"
          aria-label="Thread context"
          className="flex flex-1 flex-wrap gap-2 text-xs text-muted"
        >
          {context}
        </div>
        {runId ? (
          <Button disabled={stop.isPending} onClick={() => stop.mutate(runId)}>
            Stop
          </Button>
        ) : (
          <Button type="submit" disabled={disabled || send.isPending || !text.trim()}>
            Send
          </Button>
        )}
      </div>
      {(send.isError || stop.isError) && (
        <p className="text-sm text-danger">Could not complete the request</p>
      )}
    </form>
  );
}
