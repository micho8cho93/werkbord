import '../../../internal/uipresentation/static/work-queue.js';
import '../../../internal/uipresentation/static/work-order.js';

/** A completed move in a Work list: the ids that now sit directly above and below the item ('' at an edge), and the list it was in. */
export interface OrderDrop { id: string; before: string; after: string; from: number; to: number; scope: string }

/** Reports each completed drag or Alt+Arrow move inside the node. The product checks permission and persists the new order. */
export function orderDrops(node: HTMLElement, handler: (drop: OrderDrop) => void) {
  let current = handler;
  const listener = (event: Event) => {
    const list = (event.target as HTMLElement).closest<HTMLElement>('[data-order-list]');
    current({ ...(event as CustomEvent<Omit<OrderDrop, 'scope'>>).detail, scope: list?.dataset.orderScope ?? '' });
  };
  node.addEventListener('werkbord-order-drop', listener);
  return { update(next: (drop: OrderDrop) => void) { current = next; }, destroy() { node.removeEventListener('werkbord-order-drop', listener); } };
}

/** The position between two neighbours, or null when they have no room left (spread the list instead). */
export const positionBetween = (before?: number, after?: number): number | null => WerkbordWork.order.between(before, after);
/** n evenly spaced positions, in order. */
export const spreadPositions = (count: number): number[] => WerkbordWork.order.spread(count);

/** Where `id` goes in a plan's ordered task ids after a move within the (possibly filtered) list shown: next to the neighbour it landed beside. */
export function reorderedIds(ids: readonly string[], drop: Pick<OrderDrop, 'id' | 'before' | 'after'>): string[] {
  const rest = ids.filter(id => id !== drop.id);
  const at = drop.after && rest.includes(drop.after) ? rest.indexOf(drop.after)
    : drop.before && rest.includes(drop.before) ? rest.indexOf(drop.before) + 1
    : rest.length;
  rest.splice(at, 0, drop.id);
  return rest;
}
