import { useEffect, useState, type RefObject } from 'react';

const key = (helperId: string) => `reef.helper-report-seen.${helperId}`;

const stored = (helperId: string) => {
  try {
    return localStorage.getItem(key(helperId));
  } catch {
    return null;
  }
};

// useSeenReports tells whether a helper's report message has been in the visible part of the log.
// A report is an element with data-report-for (the helper thread) and data-report-id (the message);
// what was seen is kept in the browser per helper thread. changed names the reports now rendered.
export function useSeenReports(log: RefObject<HTMLElement | null>, changed: string) {
  const [marked, setMarked] = useState<Record<string, string>>({});
  useEffect(() => {
    const root = log.current;
    if (!root) return;
    const observer = new IntersectionObserver(
      (entries) => {
        for (const { isIntersecting, target } of entries) {
          const { reportFor, reportId } = (target as HTMLElement).dataset;
          if (!isIntersecting || !reportFor || !reportId) continue;
          try {
            localStorage.setItem(key(reportFor), reportId);
          } catch {
            // the report is then seen until the page reloads
          }
          setMarked((current) => ({ ...current, [reportFor]: reportId }));
        }
      },
      { root },
    );
    root.querySelectorAll('[data-report-for]').forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, [log, changed]);
  return (helperId: string, reportId: string) =>
    marked[helperId] === reportId || stored(helperId) === reportId;
}
