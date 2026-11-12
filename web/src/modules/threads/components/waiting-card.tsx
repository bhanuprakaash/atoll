import { useSendMessage, type Run } from '@/modules/threads/api/messages';
import { mrRef } from '@/modules/threads/utils/mr-ref';
import type { Waiting } from '@/types/waiting';
import { age } from '@/utils/time';

export function readyLine(item: Waiting) {
  const checks = item.checks === 'red' && item.on_purpose ? 'red on purpose' : item.checks;
  return [mrRef(item.url), item.title, checks, item.since && age(item.since)]
    .filter(Boolean)
    .join(' · ');
}

export const waitingText = (item: Waiting) =>
  item.kind === 'decision' ? item.question : item.kind === 'blocker' ? item.text : readyLine(item);

export function WaitingCard({
  threadId,
  items,
  onSent,
}: {
  threadId: string;
  items: Waiting[];
  onSent: (run: Run) => void;
}) {
  const send = useSendMessage(threadId);
  if (items.length === 0) return null;
  const decision = items.find((item) => item.kind === 'decision');
  const blocker = items.find((item) => item.kind === 'blocker');
  const ready = items.filter((item) => item.kind === 'ready');
  return (
    <section
      aria-label="Waiting on you"
      className="mb-2 flex flex-col gap-3 rounded-3xl border border-line bg-card px-4.5 py-3.5 text-sm"
    >
      {decision && (
        <div className="flex flex-col gap-2.5">
          <p>{decision.question}</p>
          <div className="flex flex-wrap items-center gap-2">
            {decision.options?.map((option) => (
              <button
                key={option}
                type="button"
                disabled={send.isPending}
                onClick={() =>
                  send.mutate({ text: option }, { onSuccess: ({ run }) => run && onSent(run) })
                }
                className="h-8 rounded-full border border-outline px-3.5 text-[13.5px]"
              >
                {option}
              </button>
            ))}
            <span className="text-[13px] text-muted">or reply below</span>
          </div>
          {send.isError && <p className="text-[13px] text-danger">{send.error.message}</p>}
        </div>
      )}
      {blocker && (
        <p className="relative pl-4">
          <span
            aria-hidden="true"
            className="absolute top-[0.5lh] left-0 size-2 -translate-y-1/2 rounded-full bg-blocked"
          />
          <span className="line-clamp-2">{blocker.text}</span>
        </p>
      )}
      {ready.length > 0 && (
        <ul aria-label="Ready to merge" className="flex flex-col gap-1.5">
          {ready.map((item) => (
            <li key={item.url} className="flex items-center gap-2.5">
              <span className="min-w-0 flex-1 truncate">
                {readyLine(item)}
                <span className="font-mono text-xs text-muted">
                  {' · '}
                  {item.sha?.slice(0, 7)}
                  {item.changed && ' · changed since ready'}
                </span>
              </span>
              <a
                href={item.url}
                target="_blank"
                rel="noreferrer"
                className="text-muted underline decoration-outline underline-offset-3"
              >
                Open
              </a>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
