import type { ToolKind } from '@/components/creature';

const verbs: Record<ToolKind, string> = {
  none: 'Thinking',
  search: 'Searching',
  read: 'Reading',
  edit: 'Editing',
  command: 'Running',
  memory: 'Remembering',
  platform: 'Setting up',
  work: 'Working',
  other: 'Working',
};

export const toolVerb = (tool?: { kind: ToolKind; verb: string }) =>
  tool?.verb || verbs[tool?.kind ?? 'none'] || 'Thinking';

export const toolLine = (tool: { kind: ToolKind; verb: string; target: string }) =>
  `${toolVerb(tool)} ${tool.target}`.trim();
