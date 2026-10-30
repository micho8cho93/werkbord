export {};
declare global { var WerkbordWork: {
  group(item: {done?: boolean; active?: boolean; attention?: boolean; blocked?: boolean; priority?: string; scheduledAt?: string; start?: string}, now?: number): 'now' | 'next' | 'later' | 'done';
  /** Ordering helpers from work-order.js: the position between two neighbours (null when there is no room left) and n evenly spaced positions. */
  order: { between(before?: number, after?: number): number | null; spread(count: number): number[] };
}; }
