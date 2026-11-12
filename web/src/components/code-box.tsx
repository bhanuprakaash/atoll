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
const _icon = 'rounded-md p-1 text-muted hover:text-ink';
const _svg = {
  'aria-hidden': true,
  width: 14,
  height: 14,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
} as const;

export function CodeBox({ source, lang }: { source: string; lang?: string }) {
  const [all, setAll] = useState(false);
  const [copied, setCopied] = useState(false);
  const [wrap, setWrap] = useState(false);
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
        <div className="flex items-center gap-0.5">
          <button
            type="button"
            aria-label="Wrap"
            aria-pressed={wrap}
            className={`${_icon} ${wrap ? 'text-ink' : ''}`}
            onClick={() => setWrap(!wrap)}
          >
            <svg {..._svg}>
              <path d="M3 6h18M3 12h14a3 3 0 0 1 0 6h-4M3 18h5" />
              <path d="m11 15-3 3 3 3" />
            </svg>
          </button>
          <button
            type="button"
            aria-label="Copy"
            className={_icon}
            onClick={() => navigator.clipboard.writeText(source).then(() => setCopied(true))}
          >
            <svg {..._svg}>
              {copied ? (
                <path d="m5 12.5 4.5 4.5L19 7.5" />
              ) : (
                <>
                  <rect x="9" y="9" width="11" height="11" rx="2" />
                  <path d="M5 15V6a2 2 0 0 1 2-2h8" />
                </>
              )}
            </svg>
          </button>
        </div>
      </div>
      <pre
        className={`px-3 pb-3 pt-1 font-mono text-[13px] leading-[1.55] ${wrap ? 'whitespace-pre-wrap break-words' : 'overflow-auto'}`}
      >
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
