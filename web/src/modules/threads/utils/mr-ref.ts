export function mrRef(url?: string) {
  const pull = url?.match(/.*\/pull\/(\d+)/)?.[1];
  if (pull) return `#${pull}`;
  const iid = url?.match(/(?:merge_requests|merge-requests)\/(\d+)/)?.[1];
  return iid && `!${iid}`;
}
