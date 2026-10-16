import { SvelteMap } from 'svelte/reactivity';
import { api } from '../api';
import { app } from '../state.svelte';
import type { ControllerEvent, Run } from '../types';
import { deckApi, type DeckExecution } from './layout';
import { mergeEvents, terminal } from './workflow-progress';

export interface Execution extends DeckExecution { run?: Run; events: ControllerEvent[]; error: string; stopping: boolean; stopRequested: boolean; caughtUp: boolean }
export class Workflows {
  executions = $state<Execution[]>([]);
  selected = $state('');
  dismissed = $state<string[]>([]);
  starting = $state<{name: string; requestId: string} | null>(null);
  error = $state('');
  private watching = new SvelteMap<string, () => void>();
  private refreshing: Promise<void> | null = null;
  private generation = 0;
  open(taskId: string) { this.selected = taskId; }
  async refresh(recoverCompleted = true): Promise<void> {
    const generation = this.generation;
    if (this.refreshing) { await this.refreshing; if (generation === this.generation) return this.refresh(recoverCompleted); return; }
    this.refreshing = (async () => {
      try {
        const history = await deckApi.history();
        if (generation !== this.generation) return;
        this.executions = history.map(h => {
          const existing = this.executions.find(x => x.taskId === h.taskId);
          return existing ? Object.assign(existing, h) : {...h, events:[], error:'', stopping:false, stopRequested:false, caughtUp:false};
        });
        await Promise.all(this.executions.map(x => this.catchUp(x, generation, recoverCompleted)));
        this.error = '';
      } catch (err) { this.error = String(err instanceof Error ? err.message : err); }
    })().finally(() => { this.refreshing = null; });
    return this.refreshing;
  }
  private async catchUp(x: Execution, generation: number, recoverCompleted: boolean) {
    try {

      const runs = await api.listTaskRuns(x.projectId, x.taskId);
      if (generation !== this.generation) return;
      // Task retries remain visible as the newest attempt of this workflow.
      const run = [...runs].sort((a,b) => b.createdAt.localeCompare(a.createdAt))[0];
      if (!run) { x.error = 'No run was started for this repository. Check the workflow task before retrying.'; return; }
      if (x.run?.id !== run.id) {
        if (x.run) { this.watching.get(x.run.id)?.(); this.watching.delete(x.run.id); }
        x.events = []; x.caughtUp = false; x.stopRequested = false;
      } else if (terminal(x.run) && x.caughtUp && !x.error && !recoverCompleted) return;
      const knownSeq = x.caughtUp ? (x.events.at(-1)?.seq ?? 0) : 0;
      if (!x.run || x.run.id !== run.id || run.version >= x.run.version) x.run = run;
      if (!this.watching.has(run.id)) this.watching.set(run.id, app.watchRun(run.id, ev => {
        if (x.run?.id !== run.id) return;
        x.events = mergeEvents(x.events, [ev]);
        const updated = (ev.payload as {run?: Run} | undefined)?.run;
        if (updated && (!x.run || updated.version >= x.run.version)) x.run = updated;
      }));
      let before = 0;
      do {
        const page = await api.runEvents(x.projectId, run.id, before, 1000);
        if (generation !== this.generation) return;
        x.events = mergeEvents(x.events, page.events);
        if (!page.hasMore || !page.events.length || page.events.some(e => e.seq <= knownSeq)) break;
        const next = Math.min(...page.events.map(e => e.seq));
        if (before && next >= before) break;
        before = next;
      } while (before > 0);
      const latest = await api.getRun(x.projectId, run.id);
      if (generation !== this.generation) return;
      if (!x.run || latest.version >= x.run.version) x.run = latest;
      x.caughtUp = true;
      x.error = '';
    } catch (err) { x.error = err instanceof Error ? err.message : String(err); }
  }
  async stopWorkflow(requestId: string) {
    await Promise.all(this.executions.filter(x => x.requestId === requestId).map(x => this.stop(x)));
  }
  async stop(x: Execution) {
    if (!x.run || terminal(x.run) || x.stopping) return;
    x.stopping = true; x.error = ''; x.stopRequested = true;
    try { const run = await api.stopRun(x.projectId, x.run.id); if (!x.run || run.version >= x.run.version) x.run = run; }
    catch (err) { x.error = `Cancellation cannot be confirmed. The runner may still be working. ${err instanceof Error ? err.message : String(err)}`; }
    finally { x.stopping = false; }
  }
  dispose() { this.generation++; for (const unwatch of this.watching.values()) unwatch(); this.watching.clear(); this.executions = []; this.selected = ''; this.dismissed = []; }
}
export const workflows = new Workflows();
