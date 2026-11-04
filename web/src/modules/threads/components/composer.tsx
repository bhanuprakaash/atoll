import { useState, type FormEvent } from 'react';
import { Button } from '@/components/button';
import { controlClass, Field } from '@/components/field';
import { useSendMessage, type Run } from '@/modules/threads/api/messages';

export function Composer({
  threadId,
  busy,
  onSent,
}: {
  threadId: string;
  busy: boolean;
  onSent: (run: Run) => void;
}) {
  const [text, setText] = useState('');
  const send = useSendMessage(threadId);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    send.mutate(text, {
      onSuccess: ({ run }) => {
        setText('');
        onSent(run);
      },
    });
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-2 border-t border-line p-4">
      <Field label="Message">
        {(id) => (
          <textarea
            id={id}
            className={controlClass}
            rows={3}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        )}
      </Field>
      {send.isError && <p className="text-sm text-danger">{send.error.message}</p>}
      <Button type="submit" disabled={busy || send.isPending || !text.trim()} className="self-end">
        Send
      </Button>
    </form>
  );
}
