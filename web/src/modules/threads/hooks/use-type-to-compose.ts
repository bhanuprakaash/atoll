import { useEffect } from 'react';

// A printable key pressed with nothing focused goes into the open thread's composer, unless a
// dialog or menu is open.
export function useTypeToCompose() {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        event.isComposing ||
        event.key.length !== 1 ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        (document.activeElement && document.activeElement !== document.body) ||
        document.querySelector('[role="dialog"], [role="menu"]')
      )
        return;
      document.querySelector<HTMLTextAreaElement>('textarea[data-composer]')?.focus();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);
}
