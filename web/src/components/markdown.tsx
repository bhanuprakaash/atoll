import ReactMarkdown from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';

export function Markdown({ children }: { children: string }) {
  return (
    <div className="flex flex-col gap-2 break-words [&_a]:underline [&_ol]:list-decimal [&_ol]:pl-5 [&_ul]:list-disc [&_ul]:pl-5 [&_pre]:overflow-auto">
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkBreaks]}>{children}</ReactMarkdown>
    </div>
  );
}
