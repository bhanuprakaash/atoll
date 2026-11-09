import { isMap, isScalar, parseDocument } from 'yaml';

export function split(content: string) {
  const m = content.match(/^---\r?\n([\s\S]*?)\r?\n?---(?:\r?\n|$)([\s\S]*)$/);
  return m ? { head: m[1], body: m[2] } : { head: '', body: content };
}

export const join = (head: string, body: string) => (head ? `---\n${head}\n---\n${body}` : body);

export function skillDescription(content: string) {
  const { head } = split(content);
  if (!head) return '';
  const doc = parseDocument(head);
  if (doc.errors.length) {
    return (head.match(/^description:[ \t]*(.*)$/m)?.[1] ?? '').trim().replace(/^["']|["']$/g, '');
  }
  const value = isMap(doc.contents) ? doc.get('description') : undefined;
  return typeof value === 'string' ? value.replace(/\n$/, '') : '';
}

export function setSkillDescription(head: string, value: string) {
  const doc = parseDocument(head);
  const text = JSON.stringify(value);
  const line = /^description:[^\r\n]*(?:\r?\n[ \t]+[^\r\n]*)*/m;
  if (doc.errors.length) {
    return line.test(head)
      ? head.replace(line, () => `description: ${text}`)
      : `description: ${text}${head ? `\n${head}` : ''}`;
  }
  const node = isMap(doc.contents) ? doc.get('description', true) : undefined;
  if (isScalar(node) && node.range) {
    const [start, end] = node.range;
    if (node.value === null) {
      const key = head.lastIndexOf('description:', start);
      const space = head[end] === '#' ? ' ' : '';
      return head.slice(0, key) + `description: ${text}${space}` + head.slice(end);
    }
    const newline = head.slice(start, end).endsWith('\n') ? '\n' : '';
    return head.slice(0, start) + text + newline + head.slice(end);
  }
  return line.test(head)
    ? head.replace(line, () => `description: ${text}`)
    : `description: ${text}${head ? `\n${head}` : ''}`;
}
