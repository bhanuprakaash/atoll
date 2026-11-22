import { useEffect, useRef, useState } from 'react';
import { ErrorNotice } from '@/components/error-notice';
import { SidePanel } from '@/components/side-panel';
import { highlight, type Token, type TokenKind } from '@/utils/highlight';
import type { FileRef } from '@/utils/file-ref';
import { useThreadFile } from '@/modules/threads/api/thread-file';

const _images = /\.(png|jpe?g|gif|webp)$/i;
const _colour: Record<TokenKind, string> = {
  comment: 'text-syn-comment',
  string: 'text-syn-string',
  number: 'text-syn-number',
  keyword: 'text-syn-keyword',
};
const action =
  'h-8 rounded-full border border-outline px-3 text-[13px] text-ink disabled:opacity-50';

// lines splits the highlighted file into lines, cutting tokens that span several.
function lines(source: string, lang: string) {
  const out: Token[][] = [[]];
  for (const t of highlight(source.replace(/\n$/, ''), lang)) {
    t.text.split('\n').forEach((part, i) => {
      if (i > 0) out.push([]);
      if (part) out[out.length - 1].push({ text: part, kind: t.kind });
    });
  }
  return out;
}

function Code({
  source,
  path,
  start,
  end,
}: { source: string; path: string } & Omit<FileRef, 'path'>) {
  const first = useRef<HTMLDivElement>(null);
  useEffect(() => first.current?.scrollIntoView({ block: 'center' }), [source, start]);
  const lang = path.slice(path.lastIndexOf('.') + 1);
  return (
    <pre className="min-w-max pb-4 font-mono text-[12.5px] leading-[1.6]">
      {lines(source, lang).map((tokens, i) => {
        const n = i + 1;
        const tinted = start !== undefined && n >= start && n <= (end ?? start);
        return (
          <div
            key={i}
            ref={n === start ? first : undefined}
            data-line={n}
            data-tinted={tinted || undefined}
            className={`flex pr-6 ${tinted ? 'bg-selected' : ''}`}
          >
            <span className="w-14 flex-none pr-4 text-right text-label select-none">{n}</span>
            <code>
              {tokens.map((t, j) =>
                t.kind ? (
                  <span key={j} className={_colour[t.kind]}>
                    {t.text}
                  </span>
                ) : (
                  t.text
                ),
              )}
            </code>
          </div>
        );
      })}
    </pre>
  );
}

// FilePanel shows a file of the thread's checkout, read-only, as it is now.
export function FilePanel({
  threadId,
  file,
  onClose,
}: {
  threadId: string;
  file: FileRef;
  onClose: () => void;
}) {
  const [picked, setPicked] = useState('');
  const path = picked || file.path;
  const image = _images.test(path);
  const { data, error, refetch, isFetching } = useThreadFile(threadId, path, !image);
  const shown = data?.path ?? path;
  return (
    <SidePanel
      label="File"
      onClose={onClose}
      header={
        <>
          <span className="min-w-0 truncate font-mono text-[12.5px] text-ink">{shown}</span>
          {data?.branch && <span className="flex-none text-muted">{data.branch}</span>}
          {data?.commit && <span className="flex-none text-muted">at {data.commit}</span>}
        </>
      }
      actions={
        !image && (
          <button
            type="button"
            disabled={isFetching}
            onClick={() => void refetch()}
            className={`${action} flex-none`}
          >
            Reload
          </button>
        )
      }
    >
      {image ? (
        <img
          src={`/api/threads/${threadId}/file?path=${encodeURIComponent(path)}`}
          alt={path}
          className="max-w-full px-6"
        />
      ) : data?.matches ? (
        <div className="flex flex-col gap-2 px-6 py-2 text-[13.5px]">
          <p className="text-muted">
            {data.matches.length} files are named {file.path}. Pick one:
          </p>
          <ul aria-label="Matching files" className="-mx-2 flex flex-col">
            {data.matches.map((m) => (
              <li key={m}>
                <button
                  type="button"
                  onClick={() => setPicked(m)}
                  className="w-full rounded-lg px-2 py-1.5 text-left font-mono text-[12.5px] hover:bg-selected"
                >
                  {m}
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : data?.text !== undefined ? (
        <Code source={data.text} path={shown} start={file.start} end={file.end} />
      ) : error ? (
        <div className="px-6 py-2">
          <ErrorNotice error={error} next="" details="" />
        </div>
      ) : (
        <p className="px-6 py-2 text-[13.5px] text-muted">Loading {path}…</p>
      )}
    </SidePanel>
  );
}
