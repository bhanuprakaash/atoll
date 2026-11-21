const prefix = 'atoll.draft.';

export function readDraft(key: string) {
  try {
    return localStorage.getItem(prefix + key) ?? '';
  } catch {
    return '';
  }
}

export function saveDraft(key: string, text: string) {
  try {
    if (text) localStorage.setItem(prefix + key, text);
    else localStorage.removeItem(prefix + key);
  } catch {
    // storage unavailable: the draft lasts until the composer closes
  }
}
