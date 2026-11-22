import ReactMarkdown, { defaultUrlTransform, type Options } from 'react-markdown';
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

// asTyped keeps constructs a person rarely means in chat (and pasted logs are full of) as plain text.
function asTyped(this: { data: () => { micromarkExtensions?: unknown[] } }) {
  const data = this.data();
  (data.micromarkExtensions ??= []).push({
    disable: { null: ['headingAtx', 'setextUnderline', 'thematicBreak', 'codeIndented', 'table'] },
  });
}

const _richPlugins: Options['remarkPlugins'] = [remarkGfm, remarkBreaks];
const _typedPlugins = [remarkGfm, asTyped] as Options['remarkPlugins'];

type Node = { type: string; tagName?: string; value?: string; children?: Node[] };
const isBlock = (n?: Node) =>
  n?.type === 'element' && /^(p|ul|ol|li|pre|blockquote)$/.test(n.tagName ?? '');

// dropGaps removes the newlines between blocks, which a message's kept spaces would show as blank
// lines.
function dropGaps(node: Node) {
  if (!node.children) return;
  node.children = node.children.filter(
    (c, i, all) =>
      !(c.type === 'text' && !c.value?.trim() && (isBlock(all[i - 1]) || isBlock(all[i + 1]))),
  );
  node.children.forEach(dropGaps);
}

const _typedRehype = [() => dropGaps] as Options['rehypePlugins'];

function Fence({ lang, source, plain }: { lang: string; source: string; plain?: boolean }) {
  if (!plain) {
    if (lang === 'diff') return <DiffView source={source} />;
    if (lang === 'reef-metrics') {
      const items = parseMetrics(source);
      if (items) return <Metrics items={items} />;
    }
    if (lang === 'reef-kv') {
      const pairs = parseKv(source);
      if (pairs) return <KeyValue pairs={pairs} />;
    }
  }
  return (
    <CodeBox source={source} lang={lang.startsWith('reef-') ? undefined : lang || undefined} />
  );
}

// onOpenPR, when given, turns a link to a GitHub pull request into a chip that opens the PR panel;
// onOpenFile turns inline code that names a file into a link that opens the file panel.
// typed renders a person's own message: a short set, no rich blocks or images, spaces kept.
export function Markdown({
  children,
  threadId,
  onOpenPR,
  onOpenFile,
  typed,
}: {
  children: string;
  threadId?: string;
  onOpenPR?: (url: string) => void;
  onOpenFile?: (file: FileRef) => void;
  typed?: boolean;
}) {
  return (
    <div
      className={`${typed ? 'whitespace-pre-wrap [&_[data-component=code]]:text-ink' : ''} flex flex-col gap-3 break-words [&_li+li]:mt-1 [&_li>:is(ul,ol)]:mt-1 [&>:is(h1,h2,h3,h4)]:-mb-1.5 [&>:is(h1,h2,h3,h4)]:mt-2 [&>:is(h1,h2,h3,h4):first-child]:mt-0 [&_a]:underline [&_ol]:list-decimal [&_ol]:pl-5 [&_ul]:list-disc [&_ul]:pl-5 [&_pre]:overflow-auto`}
    >
      <ReactMarkdown
        remarkPlugins={typed ? _typedPlugins : _richPlugins}
        rehypePlugins={typed ? _typedRehype : undefined}
        urlTransform={(url, key) => (key === 'src' ? url : defaultUrlTransform(url))}
        components={{
          pre: ({ node }) => {
            const code = node?.children[0];
            if (code?.type !== 'element') return null;
            const cls = code.properties.className;
            const lang = Array.isArray(cls) ? String(cls[0] ?? '').replace(/^language-/, '') : '';
            const source = code.children.map((c) => (c.type === 'text' ? c.value : '')).join('');
            return <Fence lang={lang} source={source.replace(/\n$/, '')} plain={typed} />;
          },
          code: ({ children }) => {
            const file = onOpenFile && typeof children === 'string' ? fileRef(children) : undefined;
            const code = (
              <code
                className={`rounded-control px-1 text-[0.88em] ${typed ? 'bg-accent-ink/10' : 'bg-tile'}`}
              >
                {children}
              </code>
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
            if ((data && !typed) || (threadId && !/^([a-z][a-z0-9+.-]*:|\/\/)/i.test(src)))
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
