// What the Work page's Overview reads in the web app: the workspace summary (the same one the Mac window reads through the app), made
// into the overview shape the shared Work components take, with the decisions waiting on the person kept the way the window keeps them.
import { api } from '../api';
import { DecisionQueue } from '../../shell/decisions.svelte';
import type { Item, Overview, Summary } from '../../shell/types';

/** The person's own Werkbord, as a workspace: the web app has only this one. */
export const PERSONAL: Item = { id: 'personal', kind: 'personal', name: 'Individual', state: 'ready', source: 'local' };

class WorkSummary {
  summary = $state<Summary | null>(null);
  /** The last time a summary was read, for deciding whether an answer can still be offered. */
  readAt = $state(0);
  failed = $state(false);
  readonly decisions = new DecisionQueue();
  private pending: Promise<void> | null = null;
  private last = 0;

  readonly overview = $derived<Overview | null>(this.summary ? { entries: [{ workspace: PERSONAL, summary: this.summary }], at: this.summary.at } : null);

  /** Reads the summary again: at most once a second however often it is asked. */
  refresh(): Promise<void> {
    if (this.pending) return this.pending;
    if (Date.now() - this.last < 1000) return Promise.resolve();
    this.last = Date.now();
    this.pending = api.workspaceSummary()
      .then((s) => { this.summary = s; this.readAt = Date.now(); this.failed = false; this.decisions.sync(this.overview); })
      .catch(() => { this.failed = true; })
      .finally(() => { this.pending = null; });
    return this.pending;
  }
}

export const workSummary = new WorkSummary();
