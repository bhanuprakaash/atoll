import { cloneElement, isValidElement, type ReactNode } from 'react';
import type { ExtraProps } from 'react-markdown';

type Node = NonNullable<ExtraProps['node']>;
type Item = { checked: boolean; label: ReactNode };

export function isTaskList(node: Node) {
  const items = node.children.filter((c) => c.type === 'element');
  return (
    items.length > 0 &&
    items.every(
      (li) =>
        li.type === 'element' &&
        li.tagName === 'li' &&
        Array.isArray(li.properties.className) &&
        li.properties.className.includes('task-list-item') &&
        !li.children.some(
          (c) => c.type === 'element' && (c.tagName === 'ul' || c.tagName === 'ol'),
        ),
    )
  );
}

function split(node: ReactNode, found: { checked?: boolean }): ReactNode {
  if (Array.isArray(node)) return node.map((n) => split(n, found));
  if (!isValidElement<{ children?: ReactNode; checked?: boolean }>(node)) return node;
  if (node.type === 'input') {
    found.checked = Boolean(node.props.checked);
    return null;
  }
  return node.props.children === undefined
    ? node
    : cloneElement(node, undefined, split(node.props.children, found));
}

function items(children: ReactNode): Item[] {
  return (Array.isArray(children) ? children : [children])
    .filter(isValidElement<{ children?: ReactNode }>)
    .map((li) => {
      const found: { checked?: boolean } = {};
      const label = split(li.props.children, found);
      return { checked: Boolean(found.checked), label };
    });
}

export function Checklist({ children }: { children?: ReactNode }) {
  return (
    <div data-component="checklist" role="group" className="flex flex-col gap-2">
      {items(children).map(({ checked, label }, i) => (
        <div
          key={i}
          role="checkbox"
          aria-checked={checked}
          aria-readonly="true"
          className={`flex items-start gap-2.5 ${checked ? 'text-muted' : ''}`}
        >
          <span
            aria-hidden="true"
            className={`mt-[3px] flex size-4 flex-none items-center justify-center rounded-[5px] border ${checked ? 'border-ok bg-ok-bg text-ok' : 'border-outline'}`}
          >
            {checked && (
              <svg
                viewBox="0 0 12 12"
                className="size-2.5"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <path d="m2.5 6.2 2.2 2.2 4.8-5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            )}
          </span>
          <span className="min-w-0">{label}</span>
        </div>
      ))}
    </div>
  );
}
