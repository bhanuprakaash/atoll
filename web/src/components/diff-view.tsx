type Kind = 'add' | 'del' | 'ctx' | 'hunk';
type Row = { kind: Kind; text: string };
type File = { path?: string; add: number; del: number; rows: Row[] };

function parse(source: string): File[] {
  const files: File[] = [];
  let file: File | undefined;
  let header = false;
  const start = (path?: string) => {
    file = { path, add: 0, del: 0, rows: [] };
    files.push(file);
  };
  const lines = source.split('\n');
  lines.forEach((line, i) => {
    const git = /^diff --git a\/.* b\/(.*)$/.exec(line);
    if (git) {
      start(git[1]);
      header = true;
    } else if (line.startsWith('--- ') && lines[i + 1]?.startsWith('+++ ')) {
      if (!header) start();
      header = true;
    } else if (header && line.startsWith('+++ ')) {
      const path = line.slice(4).replace(/^b\//, '');
      if (file && path !== '/dev/null') file.path = path;
    } else if (header && !line.startsWith('@@')) {
      return;
    } else if (line.startsWith('@@')) {
      header = false;
      if (!file) start();
      file!.rows.push({ kind: 'hunk', text: line });
    } else if (line.startsWith('\\')) {
      return;
    } else {
      if (!file) start();
      const kind = line.startsWith('+') ? 'add' : line.startsWith('-') ? 'del' : 'ctx';
      if (kind === 'add') file!.add++;
      if (kind === 'del') file!.del++;
      file!.rows.push({ kind, text: line.slice(1) });
    }
  });
  return files;
}

const _row: Record<Kind, string> = {
  add: 'bg-ok-bg text-ok',
  del: 'bg-del-bg text-needs-ink',
  ctx: 'text-ink/80',
  hunk: 'bg-box text-muted',
};
const _mark: Record<Kind, string> = { add: '+', del: '-', ctx: ' ', hunk: '' };

export function DiffView({ source }: { source: string }) {
  const files = parse(source);
  return (
    <div
      data-component="diff"
      className="overflow-auto rounded-xl border border-line bg-well font-mono text-[13px] leading-[1.55]"
    >
      <div className="min-w-max">
        {files.map((f, i) => (
          <div key={i}>
            {f.path && (
              <div
                data-kind="file"
                className="flex items-center gap-3 border-b border-line bg-box px-3 py-1.5 text-ink"
              >
                <span className="min-w-0 flex-1">{f.path}</span>
                <span className="text-ok">+{f.add}</span>
                <span className="text-needs-ink">-{f.del}</span>
              </div>
            )}
            {f.rows.map((r, j) => (
              <div key={j} data-kind={r.kind} className={`whitespace-pre px-3 ${_row[r.kind]}`}>
                {r.kind === 'hunk' ? r.text : `${_mark[r.kind]}${r.text}`}
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
