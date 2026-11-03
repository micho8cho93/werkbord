// What state each row of the cross-project Work queue is in, said once, and the two counts that sit beside Now and Next. A task that
// needs attention is not the same thing as a pending decision (decisions.ts): the counts here are of tasks, and say so.

import { actionable, executionLabel } from './aggregate';
import type { Attention, Execution, Overview, QueueItem } from './types';

export type RowState = 'action' | 'running' | 'blocked' | 'waiting' | 'done' | 'ready';

export const STATE_LABEL: Record<RowState, string> = {
  action: 'Needs action',
  running: 'Running',
  blocked: 'Blocked',
  waiting: 'Waiting',
  done: 'Done',
  ready: 'Ready',
};

/** Why a row stands out, in the order of how much it matters: an agent waits for an answer, something failed or is blocked, work is ready for review, the task is high priority. */
export type Flag = 'ask' | 'failed' | 'review' | 'high';

export const FLAG_LABEL: Record<Flag, string> = { ask: 'Answer needed', failed: 'Failed or blocked', review: 'Ready for review', high: 'High priority' };

export interface Row {
  workspace: string;
  item: QueueItem;
  state: RowState;
  /** An agent is waiting for an answer on this task (a decision, counted in the panel; the task is also one that needs action). */
  asking: boolean;
  /** What most deserves the person's eye about this row, if anything. */
  flag?: Flag;
}

/** What each workspace says about a task, by the place that opens it: the work it reports, and what waits on a person. */
export function signals(o: Overview | null): { exec: Map<string, Execution>; attention: Map<string, Attention[]> } {
  const exec = new Map<string, Execution>();
  const attention = new Map<string, Attention[]>();
  for (const e of o?.entries ?? []) {
    for (const w of e.summary?.work ?? []) if (w.execution && !w.report?.stale) exec.set(`${e.workspace.id}|${w.href}`, w.execution);
    for (const a of e.summary?.attention ?? []) {
      if (!a.href || !actionable(a)) continue;
      const key = `${e.workspace.id}|${a.href}`;
      attention.set(key, [...(attention.get(key) ?? []), a]);
    }
  }
  return { exec, attention };
}

export function stateOf(item: QueueItem, execution: Execution | undefined, waiting: readonly Attention[]): RowState {
  if (item.group === 'done') return 'done';
  const kinds = waiting.map((a) => a.kind);
  if (kinds.includes('needs_input')) return 'action';
  if (kinds.includes('blocked') || execution === 'blocked' || item.detail?.startsWith('Blocked')) return 'blocked';
  if (waiting.length) return 'action';
  if (execution === 'running' || execution === 'starting') return 'running';
  if (execution === 'queued' || execution === 'waiting' || execution === 'idle') return 'waiting';
  return 'ready';
}

/** The one reason a task stands out: an answer it waits for, then a failure or blocker, then a review, then high priority. Done work stands out for nothing. */
export function flagOf(item: QueueItem, state: RowState, waiting: readonly Attention[]): Flag | undefined {
  if (state === 'done') return undefined;
  const kinds = waiting.map((a) => a.kind);
  if (kinds.includes('needs_input')) return 'ask';
  if (state === 'blocked' || kinds.some((k) => k === 'failed' || k === 'conflict')) return 'failed';
  if (kinds.includes('review') || kinds.includes('changes_requested') || item.detail === 'review') return 'review';
  if (item.priority === 'high') return 'high';
  return undefined;
}

export function rowsOf(o: Overview | null, items: readonly { workspace: string; item: QueueItem }[]): Row[] {
  const { exec, attention } = signals(o);
  return items.map(({ workspace, item }) => {
    const key = `${workspace}|${item.href}`;
    const waiting = attention.get(key) ?? [];
    const state = stateOf(item, exec.get(key), waiting);
    return { workspace, item, state, asking: waiting.some((a) => a.kind === 'needs_input'), flag: flagOf(item, state, waiting) };
  });
}

const FLAG_FIRST: Record<Flag, number> = { ask: 0, failed: 1, review: 2, high: 3 };
const STATE_FIRST: Record<RowState, number> = { action: 4, blocked: 4, running: 4, waiting: 5, ready: 6, done: 7 };
const rank = (r: { state: RowState; flag?: Flag }): number => (r.flag ? FLAG_FIRST[r.flag] : STATE_FIRST[r.state]);

/**
 * Within a section the order is the person's order of attention: an agent waiting for an answer, then failures and blockers, then work
 * ready for review, then high-priority tasks, then the rest of what is moving, then what is only ready, then what is done. Rows of one
 * rank keep the order the workspace gave them.
 */
export const urgentFirst = <T extends { state: RowState; flag?: Flag }>(rows: readonly T[]): T[] => rows.map((r, i) => ({ r, i })).sort((a, b) => rank(a.r) - rank(b.r) || a.i - b.i).map((x) => x.r);

/** How many rows stand out for each reason, for the line above the queue. */
export function flagCounts(rows: readonly { flag?: Flag }[]): { flag: Flag; label: string; count: number }[] {
  return (['ask', 'failed', 'review', 'high'] as const)
    .map((flag) => ({ flag, label: FLAG_LABEL[flag], count: rows.filter((r) => r.flag === flag).length }))
    .filter((c) => c.count > 0);
}

/** Tasks in Now that need the person to act. */
export const needAction = (now: readonly Row[]): number => now.filter((r) => r.state === 'action').length;
/** Tasks in Next that can be started as they are. */
export const readyCount = (next: readonly Row[]): number => next.filter((r) => r.state === 'ready').length;

/** What a row says about the work: why it is blocked, or what its agent is doing, or where it stands, in words. */
export function rowDetail(overview: Overview | null, workspace: string, item: QueueItem): string | undefined {
  if (item.detail?.startsWith('Blocked')) return item.detail;
  const execution = overview?.entries.find((e) => e.workspace.id === workspace)?.summary?.work.find((w) => w.href === item.href);
  return execution?.execution
    ? (execution.report?.stale ? 'Last reported · ' : '') + executionLabel(execution.execution)
    : item.detail?.replace('backlog', 'Ready to start').replace('in_progress', 'In progress').replace('doing', 'In progress').replace('available', 'Available');
}
