import { Link } from 'react-router';
import { Creature, type CreatureLook } from '@/components/creature';
import { useAnswerApproval } from '@/modules/threads/api/approvals';
import { approvalTarget, approvalVerb } from '@/modules/threads/components/approval-card';
import { waitingText } from '@/modules/threads/components/waiting-card';
import type {
  ApprovalNotice,
  EarlierNotice,
  Notifications,
} from '@/modules/threads/hooks/use-notifications';
import type { Waiting } from '@/types/waiting';
import { relativeTime } from '@/utils/time';

export type WaitingNotice = {
  bot_id: string;
  bot_name: string;
  thread_id: string;
  title: string;
  item: Waiting;
};

type Looks = Record<string, CreatureLook | undefined>;

const threadPath = (n: { bot_id: string; thread_id: string }) =>
  `/bots/${n.bot_id}/threads/${n.thread_id}`;

function AnswerButtons({ id, className }: { id: string; className: string }) {
  const answer = useAnswerApproval(id);
  return (
    <>
      <button
        type="button"
        disabled={answer.isPending}
        onClick={() => answer.mutate(false)}
        className={`rounded-xl border border-outline px-4 ${className}`}
      >
        Deny
      </button>
      <button
        type="button"
        disabled={answer.isPending}
        onClick={() => answer.mutate(true)}
        className={`rounded-xl bg-accent px-5 font-medium text-accent-ink ${className}`}
      >
        Allow
      </button>
    </>
  );
}

export function ApprovalDrop({
  approval,
  look,
}: {
  approval: ApprovalNotice;
  look?: CreatureLook;
}) {
  return (
    <div
      role="alert"
      className="flex flex-col gap-3 border-t border-composer-line px-4 pt-3.5 pb-4 motion-safe:animate-rise"
    >
      <div className="flex items-center gap-3">
        <Creature look={look} state="needs_you" size={32} />
        <div className="min-w-0 flex-1 text-sm">
          <div className="truncate">
            <span className="font-medium">{approval.bot_name}</span>
            <span className="text-muted"> · {approval.title}</span>
          </div>
          <div className="text-[13px] text-needs-ink">{approvalVerb(approval.tool)}</div>
        </div>
      </div>
      <div className="max-h-28 overflow-y-auto rounded-xl bg-card px-3 py-2 font-mono text-[12.5px] [overflow-wrap:anywhere]">
        {approvalTarget(approval.input)}
      </div>
      <div className="flex items-center gap-2">
        <Link to={threadPath(approval)} className="px-1 text-[13.5px] text-muted hover:text-ink">
          Open thread
        </Link>
        <span className="flex-1" />
        <AnswerButtons id={approval.id} className="h-8.5 text-[13.5px]" />
      </div>
    </div>
  );
}

export function FinishedNotice({ notice, look }: { notice: EarlierNotice; look?: CreatureLook }) {
  return (
    <div role="status" className="border-t border-composer-line motion-safe:animate-rise">
      <div className="flex items-center gap-3 px-3 py-2.5">
        <Creature look={look} size={28} />
        <span className="min-w-0 flex-1 truncate text-sm">{notice.text}</span>
        <Link to={threadPath(notice)} className="text-[13px] hover:text-muted">
          View
        </Link>
      </div>
      <div className="h-0.5 bg-composer-line">
        <div className="h-0.5 origin-left bg-kind motion-safe:animate-drain" />
      </div>
    </div>
  );
}

export function NotificationList({
  notifications: { needs, earlier },
  waiting,
  looks,
  onClose,
}: {
  notifications: Notifications;
  waiting: WaitingNotice[];
  looks: Looks;
  onClose: () => void;
}) {
  const heading = 'px-2 pt-2 pb-1 text-[11.5px] tracking-[.06em] uppercase';
  return (
    <div
      role="dialog"
      aria-label="Notifications"
      className="flex min-h-0 flex-col gap-0.5 overflow-y-auto border-t border-composer-line p-2"
    >
      {needs.length > 0 && (
        <>
          <h3 className={`${heading} text-danger`}>Needs you</h3>
          <ul aria-label="Needs you" className="flex flex-col gap-0.5">
            {needs.map((n) => (
              <li key={n.id} className="flex items-center gap-2.5 rounded-2xl bg-needs p-2">
                <Creature look={looks[n.bot_id]} state="needs_you" size={28} />
                <Link to={threadPath(n)} onClick={onClose} className="min-w-0 flex-1">
                  <span className="block truncate text-[13.5px]">
                    {n.bot_name} · {n.title}
                  </span>
                  <span className="block truncate font-mono text-xs text-needs-ink">
                    {approvalTarget(n.input)}
                  </span>
                </Link>
                <AnswerButtons id={n.id} className="h-7 text-[12.5px]" />
              </li>
            ))}
          </ul>
        </>
      )}
      {waiting.length > 0 && (
        <>
          <h3 className={`${heading} text-danger`}>Waiting on you</h3>
          <ul aria-label="Waiting on you" className="flex flex-col gap-0.5">
            {waiting.map((n) => (
              <li
                key={`${n.thread_id}:${n.item.kind}:${n.item.url ?? ''}`}
                className="flex items-center gap-2.5 rounded-2xl bg-needs p-2"
              >
                <Creature look={looks[n.bot_id]} state="needs_you" size={28} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13.5px]">
                    {n.bot_name} · {n.title}
                  </span>
                  <span className="block truncate text-xs text-needs-ink">
                    {waitingText(n.item)}
                  </span>
                </span>
                <Link
                  to={threadPath(n)}
                  onClick={onClose}
                  className="text-[13px] text-muted hover:text-ink"
                >
                  Open thread
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
      {earlier.length > 0 && (
        <>
          <h3 className={`${heading} text-label`}>Earlier</h3>
          <ul aria-label="Earlier" className="flex flex-col gap-0.5">
            {earlier.map((n) => (
              <li key={n.id}>
                <Link
                  to={threadPath(n)}
                  onClick={onClose}
                  className="flex items-center gap-2.5 rounded-2xl p-2 hover:bg-selected"
                >
                  <Creature look={looks[n.bot_id]} size={28} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[13.5px]">{n.text}</span>
                    {n.detail && (
                      <span className="block truncate text-xs text-muted">{n.detail}</span>
                    )}
                  </span>
                  <span className="text-xs text-muted">
                    {relativeTime(new Date(n.at).toISOString())}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
      {needs.length === 0 && waiting.length === 0 && earlier.length === 0 && (
        <p className="px-2 py-3 text-center text-[13px] text-muted">Nothing new</p>
      )}
    </div>
  );
}
