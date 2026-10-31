// What a task is, in a plan or on its own, said in one word and one reason. Derived from the facts a task and its latest run
// already carry (state, run, dependencies); nothing here is stored, and nothing here starts, moves or stops anything.

import { runStatus } from './format';
import { blockerLine } from './policy';
import { blockingTasks, type WorkPlan } from './work-queue';
import type { Run, Task } from './types';

export type StepKind = 'done' | 'running' | 'attention' | 'review' | 'blocked' | 'waiting' | 'ready' | 'missing';

export interface Step {
  kind: StepKind;
  /** The word the person reads: "Complete", "Running", "Ready", "Blocked", "Waiting". */
  label: string;
  /** Why, when it is not obvious. Always present for blocked and waiting work. */
  reason: string;
  /** Unfinished tasks this one waits for. */
  blockedBy: { id: string; title: string }[];
  /** The one thing the person could do about it, as a link's words; empty when there is nothing to do (done, running). */
  next: string;
}

const clip = (s: string, n = 160) => (s.length > n ? s.slice(0, n - 1) + '…' : s);

/** The state of one task, for a plan's list or a task's own page. `tasks` is every task of the project; `latest` its latest runs. */
export function stepOf(task: Task | undefined, tasks: readonly Task[], latest: Record<string, Run> = {}): Step {
  if (!task) return { kind: 'missing', label: 'Unavailable', reason: 'This task was removed or is not visible here.', blockedBy: [], next: '' };
  const run = latest[task.id];
  const waits = blockingTasks(task, tasks as Task[], latest);
  if (task.state === 'done') return { kind: 'done', label: 'Complete', reason: '', blockedBy: [], next: '' };

  if (run && runStatus(run).active) {
    const s = runStatus(run);
    if (run.state === 'blocked') return { kind: 'attention', label: 'Blocked', reason: clip(blockerLine(run)), blockedBy: [], next: 'Tell the agent how to proceed' };
    if (s.needsInput) return { kind: 'attention', label: s.label, reason: run.waiting === 'question' ? 'The agent asked a question and cannot go on without your answer.' : 'The agent finished its turn and is waiting for your reply.', blockedBy: [], next: run.waiting === 'question' ? 'Answer' : 'Reply' };
    return { kind: 'running', label: 'Running', reason: run.activity ? clip(run.activity) : s.hint, blockedBy: [], next: '' };
  }
  if (task.state === 'review') return { kind: 'review', label: 'Ready for review', reason: 'The agent finished. Check the result before it counts as done.', blockedBy: [], next: 'Review' };

  if (run?.state === 'failed' || run?.state === 'stopped') {
    const why = run.reason ? clip(run.reason) : run.state === 'failed' ? 'The last run failed.' : 'The last run was stopped.';
    return { kind: 'blocked', label: run.state === 'failed' ? 'Failed' : 'Stopped', reason: why, blockedBy: [], next: 'Look at the run' };
  }
  if (waits.length) {
    // A task a dependency cannot unblock (it failed, stopped, or is gone) is blocked; one whose dependencies are simply still in
    // progress is waiting for them.
    const stuck = waits.filter(w => {
      const t = tasks.find(x => x.id === w.id);
      const r = latest[w.id];
      return !t || r?.state === 'failed' || r?.state === 'stopped' || r?.state === 'blocked';
    });
    const names = (list: { title: string }[]) => list.map(w => `“${w.title}”`).join(', ');
    if (stuck.length) {
      return { kind: 'blocked', label: 'Blocked', reason: `${names(stuck)} did not finish, so this cannot start.`, blockedBy: waits, next: 'Open the blocking task' };
    }
    return { kind: 'waiting', label: 'Waiting', reason: `Starts after ${names(waits)}.`, blockedBy: waits, next: '' };
  }
  return { kind: 'ready', label: 'Ready', reason: '', blockedBy: [], next: task.state === 'doing' ? 'Continue' : 'Open' };
}

export type PlanStanding = 'complete' | 'attention' | 'blocked' | 'running' | 'ready' | 'empty';

export interface PlanProgress {
  total: number;
  done: number;
  standing: PlanStanding;
  /** "Complete", "Needs you", "2 blocked"…: the plan's status in a word or two. */
  label: string;
  /** Where the plan can move next: the first task that is running, needs the person or is ready; failing that, the first not complete. */
  upNext?: { id: string; step: Step };
}

/** A plan's progress and overall status, from its tasks in order. */
export function progressOf(plan: Pick<WorkPlan, 'taskIds'>, tasks: readonly Task[], latest: Record<string, Run> = {}): PlanProgress {
  const steps = plan.taskIds.map(id => ({ id, step: stepOf(tasks.find(t => t.id === id), tasks, latest) }));
  const total = steps.length;
  const done = steps.filter(s => s.step.kind === 'done').length;
  const count = (k: StepKind) => steps.filter(s => s.step.kind === k).length;
  // Where the plan can move: the first task that is running, wants the person or is ready; a task that waits is not up next while one before or after it can go.
  const upNext = steps.find(s => ['attention', 'review', 'running', 'ready'].includes(s.step.kind)) ?? steps.find(s => s.step.kind !== 'done' && s.step.kind !== 'missing');
  const attention = count('attention') + count('review');
  const blocked = count('blocked');
  let standing: PlanStanding;
  let label: string;
  if (!total) { standing = 'empty'; label = 'No tasks yet'; }
  else if (done === total) { standing = 'complete'; label = 'Complete'; }
  else if (attention) { standing = 'attention'; label = attention === 1 ? 'Needs you' : `${attention} need you`; }
  else if (blocked) { standing = 'blocked'; label = `${blocked} blocked`; }
  else if (count('running')) { standing = 'running'; label = 'In progress'; }
  else { standing = 'ready'; label = done ? 'In progress' : 'Ready'; }
  return { total, done, standing, label, upNext };
}

/** The plan a task belongs to: the first plan that lists it. A task needs none. */
export function planOfTask(taskId: string, plans: readonly WorkPlan[]): WorkPlan | undefined {
  return plans.find(p => p.taskIds.includes(taskId));
}

/** A task's own next action: what the page puts first. `kind` says how it is drawn, `label` is the button. */
export interface NextAction {
  kind: 'answer' | 'review' | 'start' | 'again' | 'move' | 'reopen' | 'none';
  label: string;
}

/** What the person can do next about a task. Never changes the task: the page performs the action when it is pressed. */
export function nextAction(task: Task, step: Step, opts: { agentWork: boolean; canStart: boolean; hasRun: boolean }): NextAction {
  if (task.archivedAt) return { kind: 'none', label: '' };
  if (step.kind === 'done') return { kind: 'reopen', label: 'Reopen task' };
  if (step.kind === 'attention') return { kind: 'answer', label: step.next };
  if (step.kind === 'review') return { kind: 'review', label: 'Mark done' };
  if (step.kind === 'running') return { kind: 'none', label: '' };
  if (step.kind === 'waiting') return { kind: 'none', label: '' };
  if (step.kind === 'blocked' && step.blockedBy.length) return { kind: 'none', label: '' };
  if (opts.agentWork && opts.canStart) return { kind: opts.hasRun ? 'again' : 'start', label: opts.hasRun ? 'Run an agent again' : 'Start an agent' };
  if (task.state === 'backlog') return { kind: 'move', label: 'Move to In progress' };
  if (task.state === 'doing') return { kind: 'move', label: 'Submit for review' };
  return { kind: 'none', label: '' };
}
