export type Waiting = {
  kind: 'decision' | 'blocker' | 'ready';
  question?: string;
  options?: string[];
  text?: string;
  url?: string;
  title?: string;
  checks?: 'green' | 'red' | 'running';
  on_purpose?: boolean;
  sha?: string;
  changed?: boolean;
  since?: string;
};
