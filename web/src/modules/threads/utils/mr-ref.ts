// headPR is the newest open PR, else the newest, of PRs listed in the order tracked.
export function headPR<T extends { state: string }>(prs: T[]) {
  return prs.filter((p) => p.state === 'opened').at(-1) ?? prs.at(-1);
}

export function mrRef(url?: string) {
  const pull = url?.match(/.*\/pull\/(\d+)/)?.[1];
  if (pull) return `#${pull}`;
  const iid = url?.match(/(?:merge_requests|merge-requests)\/(\d+)/)?.[1];
  return iid && `!${iid}`;
}
