import { isMap, isScalar, parseDocument } from 'yaml';

export function skillDescription(content: string) {
  const head = content.match(/^---\n([\s\S]*?)\n?---(?:\n|$)/)?.[1];
  if (!head) return '';
  const doc = parseDocument(head);
  const value = isMap(doc.contents) ? doc.get('description') : undefined;
  return typeof value === 'string' ? value.replace(/\n$/, '') : '';
}

export function setSkillDescription(head: string, value: string) {
  const doc = parseDocument(head);
  const node = isMap(doc.contents) ? doc.get('description', true) : undefined;
  const text = JSON.stringify(value);
  if (isScalar(node) && node.range) {
    const [start, end] = node.range;
    const newline = head.slice(start, end).endsWith('\n') ? '\n' : '';
    return head.slice(0, start) + text + newline + head.slice(end);
  }
  return `description: ${text}${head ? `\n${head}` : ''}`;
}
