// The decisions waiting on the person, kept for the window: what the last overviews said, and what the person has answered since.
// In memory, fed by the one overview refresh the window already runs; nothing here asks a workspace anything.

import { reconcile, type Decision } from './decisions';
import type { Overview } from './types';

class DecisionQueue {
  list = $state<Decision[]>([]);
  /** Answered here and not yet gone from the workspace's own summary: out of the way at once, so the next one shows without waiting. */
  answered = $state<Record<string, true>>({});

  sync(overview: Overview | null): void {
    this.list = reconcile(this.list, overview);
    // What the workspaces no longer list needs no memory of having been answered.
    const keys = new Set(this.list.map((d) => d.key));
    for (const key of Object.keys(this.answered)) if (!keys.has(key)) delete this.answered[key];
  }

  /** The workspace accepted the answer. */
  settle(key: string): void {
    this.answered[key] = true;
  }
}

export const decisions = new DecisionQueue();
