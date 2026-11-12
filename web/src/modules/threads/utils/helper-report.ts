const none = ['none', 'nothing', 'nothing.'];

export const realBlocked = (blocked?: string | null) => {
  const text = blocked?.trim();
  return text && !none.includes(text.toLowerCase()) ? text : '';
};

export const isQuestion = (text: string) => text.trim().split('\n').at(-1)?.trimEnd().endsWith('?');

export const firstLine = (text: string) =>
  text
    .split('\n')
    .map((line) => line.trim())
    .find(Boolean) ?? '';

const field = (fields: string, name: string) =>
  new RegExp(
    `(?:^|\\n)${name}: ([\\s\\S]*?)(?=\\n(?:Done|Tried|Blocked|Next): |\\nMerge \\S+ into your branch\\.$|$)`,
  ).exec(fields)?.[1] ?? '';

// helperReport is a helper's message in its boss's thread as the user reads it: the prose, the
// helper thread it came from, and for the lead's ledger its outcome, line and blocked. The
// Done/Tried/Blocked/Next lines stay in the helper thread.
export function helperReport(text: string) {
  const head = /^.+ reported back from thread (\S+)\.\n/.exec(text);
  if (head) {
    const rest = text.slice(head[0].length);
    const cut = rest.search(/\n\n(Done|Tried|Blocked|Next): |\n\nMerge \S+ into your branch\.$/);
    const prose = (cut < 0 ? rest : rest.slice(0, cut)).trim();
    const fields = cut < 0 ? '' : rest.slice(cut).trim();
    return {
      prose,
      threadId: head[1],
      state: 'Done',
      line: field(fields, 'Done').trim() || firstLine(prose),
      blocked: realBlocked(field(fields, 'Blocked')),
    };
  }
  const tail = /\n\(thread (\S+)\)$/.exec(text);
  if (tail) {
    const prose = text.slice(0, tail.index);
    const failed = /^.+'s run failed: ([\s\S]*)$/.exec(prose);
    return {
      prose,
      threadId: tail[1],
      state: failed ? 'Failed' : 'Stopped',
      line: failed ? firstLine(failed[1]) || firstLine(prose) : firstLine(prose),
      blocked: '',
    };
  }
  return { prose: text, threadId: '', state: '', line: firstLine(text), blocked: '' };
}
