const _extensions = new Set(
  (
    'go ts tsx js jsx mjs cjs md mdx json yaml yml toml sql css scss html sh bash zsh py rs rb java ' +
    'kt swift cpp hpp php lua txt mod sum lock xml svg proto graphql ini cfg conf ' +
    'png jpg jpeg gif webp'
  ).split(' '),
);
const _names = new Set(['Makefile', 'Dockerfile']);

export type FileRef = { path: string; start?: number; end?: number };

// fileRef reads inline code as a file path: the last part has a known extension or is Makefile or
// Dockerfile, with an optional :line, :start-end or :line:col (the column is ignored).
export function fileRef(text: string): FileRef | undefined {
  if (/[\s{*]|:\/\//.test(text)) return undefined;
  const m = text.match(/^(.+?)(?::(\d+)(?:-(\d+)|:\d+)?)?$/);
  if (!m) return undefined;
  const path = m[1];
  const name = path.slice(path.lastIndexOf('/') + 1);
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : '';
  if (!_names.has(name) && !(name.indexOf('.') > 0 && _extensions.has(ext))) return undefined;
  const start = m[2] ? Number(m[2]) : undefined;
  const end = m[3] ? Number(m[3]) : start;
  return { path, start, end };
}
