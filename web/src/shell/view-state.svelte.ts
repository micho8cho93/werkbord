// What the window's own pages remember between visits, so that going to a task and Back finds Work as it was left: the section, the
// search and whose work is shown on Work, and how far each page was scrolled. In memory only: it is the window's, not the person's data,
// and a window that is reopened starts on Work at the top.

export type WorkSection = 'queue' | 'plans';

class ViewState {
  work = $state<{ section: WorkSection; search: string; everyone: boolean }>({ section: 'queue', search: '', everyone: false });
  scroll: Record<string, number> = {};
}

export const viewState = new ViewState();

/**
 * Keeps how far a page of the window is scrolled, under the page's name: set it when the page opens, saved while the person scrolls,
 * and put back when the same page is shown again (or when this element is shown another page).
 */
export function remembered(node: HTMLElement, page: string): { update(next: string): void; destroy(): void } {
  let current = page;
  const restore = () => {
    const top = viewState.scroll[current] ?? 0;
    node.scrollTop = top;
    // Content that is drawn a moment later (a list that was just fetched) is not tall enough yet.
    if (top && node.scrollTop < top) requestAnimationFrame(() => { node.scrollTop = top; });
  };
  const save = () => { viewState.scroll[current] = node.scrollTop; };
  node.addEventListener('scroll', save, { passive: true });
  restore();
  return {
    update(next) { if (next === current) return; current = next; restore(); },
    destroy() { node.removeEventListener('scroll', save); },
  };
}
