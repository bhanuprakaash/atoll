import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import { Checklist, isTaskList } from '@/components/checklist';
import { CodeBox } from '@/components/code-box';
import { KeyValue, Metrics, parseKv, parseMetrics } from '@/components/data-cards';
import { DiffView } from '@/components/diff-view';
import { PRChip, pullNumber } from '@/components/pr-chip';
import { SortTable } from '@/components/sort-table';
import { ZoomImage } from '@/components/zoom-image';
import { fileRef, type FileRef } from '@/utils/file-ref';

function Fence({ lang, source }: { lang: string; source: string }) {
  if (lang === 'diff') return <DiffView source={source} />;
  if (lang === 'reef-metrics') {
    const items = parseMetrics(source);
    if (items) return <Metrics items={items} />;
  }
  if (lang === 'reef-kv') {
    const pairs = parseKv(source);
    if (pairs) return <KeyValue pairs={pairs} />;
  }
  return (
    <CodeBox source={source} lang={lang.startsWith('reef-') ? undefined : lang || undefined} />
  );
}

// onOpenPR, when given, turns a link to a GitHub pull request into a chip that opens the PR panel;
// onOpenFile turns inline code that names a file into a link that opens the file panel.
export function Markdown({
  children,
  threadId,
  onOpenPR,
  onOpenFile,
}: {
  children: string;
  threadId?: string;
  onOpenPR?: (url: string) => void;
  onOpenFile?: (file: FileRef) => void;
}) {
  return (
    <div className="flex flex-col gap-3 break-words [&_li+li]:mt-1 [&_li>:is(ul,ol)]:mt-1 [&>:is(h1,h2,h3,h4)]:-mb-1.5 [&>:is(h1,h2,h3,h4)]:mt-2 [&>:is(h1,h2,h3,h4):first-child]:mt-0 [&_a]:underline [&_ol]:list-decimal [&_ol]:pl-5 [&_ul]:list-disc [&_ul]:pl-5 [&_pre]:overflow-auto">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkBreaks]}
        urlTransform={(url, key) => (key === 'src' ? url : defaultUrlTransform(url))}
        components={{
          pre: ({ node }) => {
            const code = node?.children[0];
            if (code?.type !== 'element') return null;
            const cls = code.properties.className;
            const lang = Array.isArray(cls) ? String(cls[0] ?? '').replace(/^language-/, '') : '';
            const source = code.children.map((c) => (c.type === 'text' ? c.value : '')).join('');
            return <Fence lang={lang} source={source.replace(/\n$/, '')} />;
          },
          code: ({ children }) => {
            const file = onOpenFile && typeof children === 'string' ? fileRef(children) : undefined;
            const code = (
              <code className="rounded-control bg-tile px-1 text-[0.88em]">{children}</code>
            );
            return file ? (
              <button
                type="button"
                data-file-link
                onClick={() => onOpenFile!(file)}
                className="cursor-pointer underline decoration-label underline-offset-2 hover:decoration-ink"
              >
                {code}
              </button>
            ) : (
              code
            );
          },
          table: ({ node, children }) => <SortTable node={node!}>{children}</SortTable>,
          ul: ({ node, children, className }) =>
            node && isTaskList(node) ? (
              <Checklist>{children}</Checklist>
            ) : (
              <ul className={className}>{children}</ul>
            ),
          img: ({ src = '', alt }) => {
            const data = /^data:/i.test(src);
            if (data || (threadId && !/^([a-z][a-z0-9+.-]*:|\/\/)/i.test(src)))
              return (
                <ZoomImage
                  src={data ? src : `/api/threads/${threadId}/file?path=${encodeURIComponent(src)}`}
                  alt={alt ?? ''}
                  className="max-w-full rounded-xl"
                />
              );
            return (
              <a href={defaultUrlTransform(src) || undefined} target="_blank" rel="noreferrer">
                {alt || src}
              </a>
            );
          },
          a: ({ href, children }) =>
            onOpenPR && href && pullNumber(href) ? (
              <PRChip url={href} onOpen={onOpenPR} />
            ) : (
              <a href={href} target="_blank" rel="noreferrer">
                {children}
              </a>
            ),
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}
