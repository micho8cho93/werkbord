// What state each row of the cross-project Work queue is in, said once, and the two counts that sit beside Now and Next. A task that
// needs attention is not the same thing as a pending decision (decisions.ts): the counts here are of tasks, and say so.

import { actionable } from './aggregate';
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

export interface Row {
  workspace: string;
  item: QueueItem;
  state: RowState;
  /** An agent is waiting for an answer on this task (a decision, counted in the panel; the task is also one that needs action). */
  asking: boolean;
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

export function rowsOf(o: Overview | null, items: readonly { workspace: string; item: QueueItem }[]): Row[] {
  const { exec, attention } = signals(o);
  return items.map(({ workspace, item }) => {
    const key = `${workspace}|${item.href}`;
    const waiting = attention.get(key) ?? [];
    return { workspace, item, state: stateOf(item, exec.get(key), waiting), asking: waiting.some((a) => a.kind === 'needs_input') };
  });
}

const FIRST: Record<RowState, number> = { action: 0, blocked: 1, running: 2, waiting: 3, ready: 4, done: 5 };

/** Within a section what needs the person comes first; otherwise the order the workspace gave is kept. */
export const urgentFirst = <T extends { state: RowState }>(rows: readonly T[]): T[] => rows.map((r, i) => ({ r, i })).sort((a, b) => FIRST[a.r.state] - FIRST[b.r.state] || a.i - b.i).map((x) => x.r);

/** Tasks in Now that need the person to act. */
export const needAction = (now: readonly Row[]): number => now.filter((r) => r.state === 'action').length;
/** Tasks in Next that can be started as they are. */
export const readyCount = (next: readonly Row[]): number => next.filter((r) => r.state === 'ready').length;
