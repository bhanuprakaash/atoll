import type { MouseEvent, ReactNode } from 'react';

// pullNumber is the number of a GitHub pull request URL; a GitLab MR has none.
export const pullNumber = (url?: string) =>
  url?.match(/^https?:\/\/[^/]+\/[^/]+\/[^/]+\/pull\/(\d+)(?:[/?#]|$)/)?.[1];

// openOnClick opens the PR panel on a plain click; Cmd- or Ctrl-click follows the link to GitHub.
export const openOnClick = (url: string, onOpen?: (url: string) => void) => (e: MouseEvent) => {
  if (!onOpen || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  onOpen(url);
};

export function PullIcon({ className = '' }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      width="13"
      height="13"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      className={`flex-none ${className}`}
    >
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="6" cy="18" r="2.5" />
      <circle cx="18" cy="18" r="2.5" />
      <path d="M6 8.5v7M18 15.5V9a3 3 0 0 0-3-3h-4m2-2.5L10.5 6 13 8.5" />
    </svg>
  );
}

// PRChip is every mention of a GitHub pull request: a click opens it in the PR panel.
export function PRChip({
  url,
  onOpen,
  tone = 'text-muted',
  children,
}: {
  url: string;
  onOpen?: (url: string) => void;
  tone?: string;
  children?: ReactNode;
}) {
  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      onClick={openOnClick(url, onOpen)}
      className="inline-flex items-center gap-1 rounded-md bg-tile px-1.5 align-baseline text-[0.92em] text-ink no-underline! hover:bg-selected"
    >
      <PullIcon className={tone} />
      PR {pullNumber(url)}
      {children}
    </a>
  );
}
