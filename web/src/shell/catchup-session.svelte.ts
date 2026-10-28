import { readVisit, VISIT_KEY, type Visit } from './catchup';
import type { Overview } from './types';

/** One recap interval per app session, independent of Home mounting/unmounting. */
export const catchUpSession = new class {
  previous = $state<Visit | null>(null);
  through = $state(Date.now());
  private cursors: Record<string, number> = {};
  private coveredAt: Record<string, number> = {};
  ingest(overview: Overview | null): void {
    for (const e of overview?.entries ?? []) {
      if (!e.error && e.workspace.state === 'ready' && e.summary?.changes) {
        this.cursors[e.workspace.id] = e.summary.changes.cursor;
        this.coveredAt[e.workspace.id] = Date.parse(e.summary.changes.through);
      }
    }
  }
  start(): () => void {
    this.previous = readVisit(localStorage);
    this.through = Date.now();
    this.cursors = { ...this.previous?.cursors };
    this.coveredAt = { ...this.previous?.coveredAt };
    let hiddenAt = 0;
    const save = () => { try { localStorage.setItem(VISIT_KEY, JSON.stringify({ at: Date.now(), cursors: this.cursors, coveredAt: this.coveredAt })); } catch { /* Keep the in-memory recap. */ } };
    const visibility = () => {
      if (document.hidden) { hiddenAt = Date.now(); save(); }
      else if (hiddenAt && Date.now() - hiddenAt >= 30 * 60_000) { this.previous = readVisit(localStorage); this.through = Date.now(); hiddenAt = 0; }
    };
    const timer = setInterval(() => { if (!document.hidden) save(); }, 15_000);
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('pagehide', save);
    return () => { save(); clearInterval(timer); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('pagehide', save); };
  }
}();
