import ReactMarkdown, { defaultUrlTransform } from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import { ZoomImage } from '@/components/zoom-image';

export function Markdown({ children, threadId }: { children: string; threadId?: string }) {
  return (
    <div className="flex flex-col gap-3.5 break-words [&_li+li]:mt-3.5 [&_a]:underline [&_ol]:list-decimal [&_ol]:pl-5 [&_ul]:list-disc [&_ul]:pl-5 [&_pre]:overflow-auto">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkBreaks]}
        urlTransform={(url, key) => (key === 'src' ? url : defaultUrlTransform(url))}
        components={{
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
          a: ({ href, children }) => (
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
