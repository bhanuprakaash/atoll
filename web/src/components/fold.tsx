import { type ReactNode, useState } from 'react';

// Fold is one quiet line of a reply: icon, summary and the main action, then a chevron. The line
// opens its children in place; the action stays outside the toggle so it works while folded.
export function Fold({
  icon,
  summary,
  action,
  children,
}: {
  icon: ReactNode;
  summary: string;
  action?: (open: boolean) => ReactNode;
  children?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const line = (
    <>
      {icon}
      <span className="min-w-0 flex-1 truncate">{summary}</span>
    </>
  );
  const style = 'flex min-w-0 flex-1 items-center gap-2.5 text-left';
  return (
    <>
      <div className="flex h-[30px] items-center gap-2.5 text-[13.5px] text-muted">
        {children ? (
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
            className={`${style} hover:text-ink`}
          >
            {line}
          </button>
        ) : (
          <div className={style}>{line}</div>
        )}
        {action?.(open)}
        {children && (
          <button
            type="button"
            tabIndex={-1}
            aria-hidden="true"
            onClick={() => setOpen(!open)}
            className={`flex-none text-muted/70 ${open ? 'rotate-90' : ''}`}
          >
            ›
          </button>
        )}
      </div>
      {open && children}
    </>
  );
}
