// Attaches only to controller-owned structured runs. Opening/closing never starts or stops a process.
import { api } from '../api';
import { app } from '../state.svelte';
import { RunFeed } from '../feed.svelte';
import type { Run, Worktree } from '../types';

export const liveRun = (r: Run) => ['starting', 'running', 'waiting_for_user', 'blocked'].includes(r.state);
export const messageableRun = (r: Run) => r.state === 'running' || r.state === 'blocked' || (r.state === 'waiting_for_user' && r.waiting === 'idle');
export class RunSession {
  run = $state<Run>();
  worktree = $state<Worktree>();
  draft = $state('');
  error = $state('');
  busy = $state(false);
  readonly feed: RunFeed;
  private dispose?: () => void;
  private generation = 0;
  constructor(readonly projectId: string, readonly runId: string) { this.feed = new RunFeed(projectId, runId); }
  get questions() { return app.questions.filter(q => q.projectId === this.projectId && q.runId === this.runId); }
  get project() { return app.project(this.projectId); }
  attach() {
    if (this.dispose) return;
    const watch = app.watchRun(this.runId, ev => {
      if (ev.projectId !== this.projectId) return;
      this.feed.push(ev);
      const run = (ev.payload as { run?: Run })?.run;
      if (run) this.update(run);
    });
    const reconnect = app.onReconnect(() => void this.refresh());
    this.dispose = () => { watch(); reconnect(); };
    void this.refresh();
  }
  private update(run: Run) {
    if (run.id !== this.runId || run.projectId !== this.projectId) return;
    if (!this.run || run.version >= this.run.version) this.run = run;
  }
  async refresh() {
    const generation = ++this.generation;
    await this.feed.load();
    try {
      const run = await api.getRun(this.projectId, this.runId);
      if (generation !== this.generation) return;
      this.update(run);
      if (run.worktreeId) this.worktree = await api.getWorktree(this.projectId, run.worktreeId);
      this.error = '';
    } catch (e) { this.error = e instanceof Error ? e.message : String(e); }
  }
  async act(action: 'send' | 'stop' | 'finish') {
    const run = this.run;
    if (!run || this.busy || !liveRun(run)) return;
    const text = this.draft.trim();
    if (action === 'send' && (!messageableRun(run) || !text)) return;
    if (action === 'finish' && (this.questions.length || !(run.state === 'blocked' || (run.state === 'waiting_for_user' && run.waiting === 'idle')))) return;
    this.busy = true; this.error = '';
    try {
      this.update(await (action === 'send' ? api.sendInput(this.projectId, this.runId, text) : action === 'stop' ? api.stopRun(this.projectId, this.runId) : api.finishRun(this.projectId, this.runId)));
      if (action === 'send') this.draft = '';
    } catch (e) { this.error = e instanceof Error ? e.message : String(e); }
    finally { this.busy = false; }
  }
  disconnect() { this.generation++; this.dispose?.(); this.dispose = undefined; }
}
