// One project's data: its tasks, the latest run of each, its run history and its
// open questions. A ProjectScope is the only place the board, the Git view and the
// Activity view read from, and it holds one project and nothing else.
//
// That is deliberate and enforced here rather than hoped for in the views:
//
//   - it is filled only from the project-scoped endpoints, which the controller
//     answers for that project alone, so it never receives another project's data;
//   - it refuses events, and refuses entities handed to it, that name another
//     project, so a stray event can never put one project's task on another's board.
//
// Switching projects is therefore just looking at another scope: nothing is
// filtered, and nothing has to be reloaded for a project already visited.

import { api } from './api';
import { oneLine } from './format';
import { QuestionBook } from './questions';
import type { AgentOutput, ControllerEvent, Question, Run, Task } from './types';

/** The more recently created of two runs of one task; on a tie, the later version. */
export function newer(a: Run | undefined, b: Run): Run {
  if (!a) return b;
  if (a.id === b.id) return a.version <= b.version ? b : a;
  return a.createdAt >= b.createdAt ? a : b;
}

/** How many runs of history a scope keeps. Older ones are a request away. */
const HISTORY_LIMIT = 200;

export class ProjectScope {
  readonly projectId: string;

  tasks = $state<Task[]>([]);
  /** The most recent run of each task, by task ID. */
  latestRun = $state<Record<string, Run>>({});
  /** The project's runs, newest first. */
  history = $state<Run[]>([]);
  /** Questions of this project that still wait for the user, oldest first. */
  questions = $state<Question[]>([]);
  loaded = $state(false);
  loading = $state(false);
  error = $state('');

  private book = new QuestionBook();
  private loadId = 0;

  constructor(projectId: string) {
    this.projectId = projectId;
  }

  /** Fetches everything from the controller, which is where it is kept. Safe to call again, for instance after a reconnect. */
  async load(): Promise<void> {
    const id = ++this.loadId;
    this.loading = true;
    try {
      const sync = this.book.beginSync();
      const [tasks, latest, history, questions] = await Promise.all([
        api.listTasks(this.projectId),
        api.listProjectRuns(this.projectId),
        api.listActivity(this.projectId, HISTORY_LIMIT),
        api.listPendingQuestions(this.projectId),
      ]);
      if (id !== this.loadId) return; // a newer load is under way and will say it better
      this.tasks = tasks.filter((t) => t.projectId === this.projectId);
      const byTask: Record<string, Run> = {};
      for (const r of latest) if (r.projectId === this.projectId) byTask[r.taskId] = newer(byTask[r.taskId], r);
      this.latestRun = byTask;
      this.history = history.filter((r) => r.projectId === this.projectId);
      if (this.book.replace(sync, questions.filter((q) => q.projectId === this.projectId))) this.questions = this.book.pending;
      this.loaded = true;
      this.error = '';
    } catch (err) {
      if (id === this.loadId) this.error = err instanceof Error ? err.message : String(err);
      throw err;
    } finally {
      if (id === this.loadId) this.loading = false;
    }
  }

  /** Replaces or inserts a task, without waiting for its event. One of another project is ignored. */
  upsertTask(task: Task): void {
    if (task.projectId !== this.projectId) return;
    const i = this.tasks.findIndex((t) => t.id === task.id);
    if (i === -1) this.tasks.push(task);
    else if (this.tasks[i].version <= task.version) this.tasks[i] = task;
  }

  /** Same for a run: the board's latest of its task, and the history. Older versions are ignored. */
  upsertRun(run: Run): void {
    if (run.projectId !== this.projectId) return;
    this.latestRun[run.taskId] = newer(this.latestRun[run.taskId], run);
    const i = this.history.findIndex((r) => r.id === run.id);
    if (i !== -1) {
      if (this.history[i].version <= run.version) this.history[i] = run;
      return;
    }
    const at = this.history.findIndex((r) => r.createdAt < run.createdAt);
    this.history.splice(at === -1 ? this.history.length : at, 0, run);
    if (this.history.length > HISTORY_LIMIT) this.history.length = HISTORY_LIMIT;
  }

  /** A question is settled (answered here or elsewhere, or closed): stop offering it, without waiting for its event. */
  resolveQuestion(q: Pick<Question, 'id' | 'projectId'>): void {
    if (q.projectId !== this.projectId) return;
    this.book.resolve(q);
    this.questions = this.book.pending;
  }

  /** The open questions of one run. */
  pendingFor(runId: string): Question[] {
    return this.questions.filter((q) => q.runId === runId);
  }

  /** Tasks whose latest run is in the given states. */
  taskCount(pred: (run: Run) => boolean): number {
    return this.tasks.filter((t) => {
      const r = this.latestRun[t.id];
      return !!r && pred(r);
    }).length;
  }

  /**
   * Applies an event from the stream. An event of another project is not this
   * scope's business and changes nothing; the return value says whether it did.
   */
  apply(ev: ControllerEvent): boolean {
    if (ev.projectId !== this.projectId) return false;
    switch (ev.type) {
      case 'task.created':
      case 'task.updated':
        this.upsertTask(ev.payload as Task);
        return true;
      case 'run.state_changed':
        this.upsertRun((ev.payload as { run: Run }).run);
        return true;
      case 'agent.output': {
        // The card's activity line follows what the agent does, ahead of the next state change.
        const out = ev.payload as AgentOutput;
        const run = ev.taskId ? this.latestRun[ev.taskId] : undefined;
        if (run && run.id === ev.runId && (out.stream === 'assistant' || out.stream === 'tool')) {
          run.activity = oneLine(out.text);
          run.activityAt = ev.createdAt;
          return true;
        }
        return false;
      }
      case 'agent.question':
      case 'question.answered':
      case 'question.cancelled':
        // Each carries the whole question, so no refetch is needed; the list is refetched on every (re)connect.
        if (this.book.apply(ev)) this.questions = this.book.pending;
        return true;
    }
    return false;
  }
}
