export {};
declare global { var WerkbordWork: {
  group(item: {done?: boolean; active?: boolean; attention?: boolean; blocked?: boolean; priority?: string; scheduledAt?: string; start?: string}, now?: number): 'now' | 'next' | 'later' | 'done';
  /** A Team plan from one member's side. Guidance only: Team starts nothing, and a start goes through the member's own approval. */
  planSteps(steps: { id: string; title: string; status: string; assigneeId?: string; workMode?: string; dependencies?: string[]; archived?: boolean; running?: boolean }[], me: string, nameOf: (id: string) => string | undefined): {
    steps: { id: string; number: number; title: string; mine: boolean; holder: string; state: 'done' | 'review' | 'unclaimed' | 'theirs' | 'yours-human' | 'running' | 'waiting' | 'ready'; waitsFor: { id: string; number: number; title: string; holder: string }[]; follows: { id: string; number: number; title: string; holder: string } | null; canStart: boolean }[];
    next: string | null; mineOpen: number; mineWaiting: number; unclaimed: number;
  };
  /** Ordering helpers from work-order.js: the position between two neighbours (null when there is no room left) and n evenly spaced positions. */
  order: { between(before?: number, after?: number): number | null; spread(count: number): number[] };
}; }
