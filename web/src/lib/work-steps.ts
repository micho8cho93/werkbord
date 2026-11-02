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

/**
 * The tasks whose failed or stopped run is what keeps this task from starting, however far back along its dependencies they are.
 * Such a task is held: nothing about it is stored, and it goes on by itself once the one before it has been run again and finished.
 */
export function reworkBefore(task: Task, tasks: readonly Task[], latest: Record<string, Run> = {}, seen = new Set<string>()): Task[] {
  if (seen.has(task.id)) return [];
  seen.add(task.id);
  const out: Task[] = [];
  for (const wait of blockingTasks(task, tasks as Task[], latest)) {
    const upstream = tasks.find(t => t.id === wait.id);
    if (!upstream) continue;
    const r = latest[upstream.id];
    if (r?.state === 'failed' || r?.state === 'stopped') out.push(upstream);
    else for (const deeper of reworkBefore(upstream, tasks, latest, seen)) if (!out.includes(deeper)) out.push(deeper);
  }
  return out;
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
    return { kind: 'blocked', label: run.state === 'failed' ? 'Failed' : 'Stopped', reason: `${why} Run it again to let the tasks after it go on.`, blockedBy: [], next: 'Look at the run' };
  }
  const rework = waits.length ? reworkBefore(task, tasks, latest) : [];
  if (rework.length) {
    const names = rework.map(t => `“${t.title}”`).join(', ');
    return { kind: 'blocked', label: 'Held', reason: `Held until ${names} ${rework.length === 1 ? 'is' : 'are'} reworked and finished.`, blockedBy: waits, next: 'Open the task that needs rework' };
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
  else if (blocked) {
    standing = 'blocked';
    const held = steps.filter(s => s.step.label === 'Held').length;
    const rework = steps.filter(s => s.step.label === 'Failed' || s.step.label === 'Stopped').length;
    label = rework ? `${rework} need${rework === 1 ? 's' : ''} rework${held ? `, ${held} held` : ''}` : held ? `${held} held` : `${blocked} blocked`;
  }
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

/** An agent on a task, as the identity layer wants it: the provider, a state it understands and whether the report is fresh. */
export interface StepAgent {
  agentId: string;
  state: 'starting' | 'running' | 'needs_input' | 'blocked' | 'completed' | 'failed' | 'stopped';
  /** Confirmed by the controller (and, for a remote run, its runner being online). Only a fresh running state turns the ring. */
  fresh: boolean;
  observedAt: string;
}

const AGENT_STATE: Record<Run['state'], StepAgent['state']> = { starting: 'starting', running: 'running', waiting_for_user: 'needs_input', blocked: 'blocked', completed: 'completed', failed: 'failed', stopped: 'stopped' };

/** The agent on a task from its latest run. `stale` is runReportStale for that run: false means the controller and the runner confirm it. */
export function agentOf(run: Run | undefined, stale: boolean): StepAgent | undefined {
  if (!run?.agentId) return undefined;
  return { agentId: run.agentId, state: AGENT_STATE[run.state] ?? 'running', fresh: !stale, observedAt: run.activityAt ?? run.updatedAt };
}

/** One step of a plan: where it stands in the order, what it waits for inside the plan, and its agent. */
export interface PlanStep {
  id: string;
  task?: Task;
  step: Step;
  /** 1-based position in the plan. */
  number: number;
  total: number;
  /** The plan steps (by number) this one waits for, from its dependencies. Empty for a step nothing orders. */
  after: number[];
  agent?: StepAgent;
  /** Running together with another running step that has no dependency path to or from it. */
  parallel: boolean;
}

/** Whether `from` reaches `to` along dependencies, through any tasks. */
function reaches(from: string, to: string, tasks: readonly Task[], seen = new Set<string>()): boolean {
  if (seen.has(from)) return false;
  seen.add(from);
  const deps = tasks.find(t => t.id === from)?.orchestration?.dependencies ?? [];
  return deps.includes(to) || deps.some(d => reaches(d, to, tasks, seen));
}

/** A plan's tasks in order with the facts the list shows. `stale` says whether a run's report is out of date (see agentOf). */
export function planSteps(plan: Pick<WorkPlan, 'taskIds'>, tasks: readonly Task[], latest: Record<string, Run> = {}, stale: (run: Run) => boolean = () => false): PlanStep[] {
  const total = plan.taskIds.length;
  const out: PlanStep[] = plan.taskIds.map((id, i) => {
    const task = tasks.find(t => t.id === id);
    const deps = task?.orchestration?.dependencies ?? [];
    const after = deps.map(d => plan.taskIds.indexOf(d) + 1).filter(n => n > 0 && n !== i + 1).sort((a, b) => a - b);
    const run = latest[id];
    return { id, task, step: stepOf(task, tasks, latest), number: i + 1, total, after, agent: agentOf(run, run ? stale(run) : false), parallel: false };
  });
  const running = out.filter(s => s.step.kind === 'running');
  for (const s of running) s.parallel = running.some(o => o !== s && !reaches(s.id, o.id, tasks) && !reaches(o.id, s.id, tasks));
  return out;
}

/** "Step 2 of 5 · After step 1", or "Step 1 of 5" when nothing orders it. */
export function stepPosition(s: Pick<PlanStep, 'number' | 'total' | 'after'>): string {
  const base = `Step ${s.number} of ${s.total}`;
  return s.after.length ? `${base} · After step${s.after.length > 1 ? 's' : ''} ${s.after.join(', ')}` : base;
}

/** The distinct agents working on a plan's steps right now, at most `max`; the rest are counted in `more`. Only live states, never a finished run. */
export function activeAgents(steps: readonly PlanStep[], max = 3): { agents: StepAgent[]; more: number } {
  const seen = new Map<string, StepAgent>();
  for (const s of steps) if (s.agent && ['starting', 'running', 'needs_input', 'blocked'].includes(s.agent.state) && !seen.has(s.agent.agentId + s.id)) seen.set(s.agent.agentId + s.id, s.agent);
  const all = [...seen.values()];
  return { agents: all.slice(0, max), more: Math.max(0, all.length - max) };
}

export interface PlanBranch { branch: string; commit?: string; taskId: string; title: string; at?: string }

/** The branches a plan's tasks worked on, one per branch name, newest run first. Never a plan-level branch: a plan has none. */
export function planBranches(steps: readonly PlanStep[], latest: Record<string, Run>): PlanBranch[] {
  const out = new Map<string, PlanBranch>();
  for (const s of steps) {
    const run = latest[s.id];
    if (!run?.branch || out.has(run.branch)) continue;
    out.set(run.branch, { branch: run.branch, commit: run.headCommit?.slice(0, 7), taskId: s.id, title: s.task?.title ?? 'Unavailable task', at: run.endedAt ?? run.updatedAt });
  }
  return [...out.values()].sort((a, b) => Date.parse(b.at ?? '') - Date.parse(a.at ?? ''));
}
