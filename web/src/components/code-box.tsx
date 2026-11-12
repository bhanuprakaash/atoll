import { useEffect, useState } from 'react';
import { highlight, type TokenKind } from '@/utils/highlight';

const _limit = 20;
const _colour: Record<TokenKind, string> = {
  comment: 'text-syn-comment',
  string: 'text-syn-string',
  number: 'text-syn-number',
  keyword: 'text-syn-keyword',
};
const _chip = 'rounded-md px-2 py-0.5 text-xs text-muted hover:text-ink';

export function CodeBox({ source, lang }: { source: string; lang?: string }) {
  const [all, setAll] = useState(false);
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  const lines = source.split('\n');
  const long = lines.length > _limit;
  const shown = long && !all ? lines.slice(0, _limit).join('\n') : source;
  return (
    <div data-component="code" className="overflow-hidden rounded-xl border border-line bg-well">
      <div className="flex items-center justify-between px-3 pt-1.5">
        <span className="font-mono text-xs text-muted">{lang}</span>
        <button
          type="button"
          className={_chip}
          onClick={() => navigator.clipboard.writeText(source).then(() => setCopied(true))}
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre className="overflow-auto px-3 pb-3 pt-1 font-mono text-[13px] leading-[1.55]">
        <code>
          {highlight(shown, lang).map((t, i) =>
            t.kind ? (
              <span key={i} className={_colour[t.kind]}>
                {t.text}
              </span>
            ) : (
              t.text
            ),
          )}
        </code>
      </pre>
      {long && (
        <button type="button" className={`${_chip} mx-1 mb-1.5`} onClick={() => setAll(!all)}>
          {all ? 'Show less' : 'Show all'}
        </button>
      )}
    </div>
  );
}
