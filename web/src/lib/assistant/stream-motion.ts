import { animate } from '../motion';

export function appendedText(previous: string, next: string): string | null {
  return next.startsWith(previous) ? next.slice(previous.length) : null;
}

/** Own only this span's text. Never delay output or replace selected text. */
export function streamedText(node: HTMLElement, initial: { text: string; live: boolean }) {
  let text = initial.text;
  const base = document.createTextNode(text);
  node.append(base);
  let disposed = false;
  let listening = false;
  const listen = () => { if (!listening) { document.addEventListener('selectionchange', compact); listening = true; } };
  const compact = () => {
    const selection = document.getSelection();
    if (selection && !selection.isCollapsed && (node.contains(selection.anchorNode) || node.contains(selection.focusNode) || selection.containsNode(node, true))) return;
    // Avoid unbounded fragment nodes during a long reply, without disturbing selection.
    if (node.childNodes.length > 1 && !node.getAnimations({ subtree: true }).length) {
      base.data = text; node.replaceChildren(base);
    }
    if (node.childNodes.length <= 1 && listening) { document.removeEventListener('selectionchange', compact); listening = false; }
  };
  return {
    update(next: typeof initial) {
      const tail = appendedText(text, next.text);
      if (next.text === text) return;
      text = next.text;
      if (tail !== null) {
        listen();
        const fragment = document.createElement('span'); fragment.textContent = tail; node.append(fragment);
        const animation = next.live ? animate(fragment, [{ opacity: 0 }, { opacity: 1 }], 120) : null;
        if (animation) animation.finished.catch(() => {}).finally(() => { if (!disposed) compact(); }); else compact();
      } else {
        // Markdown reclassification/retry replaces content directly, never replays it.
        base.data = text; node.replaceChildren(base);
      }
    },
    destroy() { disposed = true; document.removeEventListener('selectionchange', compact); },
  };
}
