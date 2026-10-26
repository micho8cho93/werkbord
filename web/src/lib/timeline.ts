// Personal supplies status names and dependencies; all timeline geometry is shared with Team.
import '../../../internal/uipresentation/static/timeline.js';
import type { Group as TimelineGroup } from '../../../internal/uipresentation/static/timeline.js';
import type { Label, Task } from './types';
export type { Span, Range, Bar, Arrow, Tick, Zoom, GroupBy } from '../../../internal/uipresentation/static/timeline.js';
export type Group = TimelineGroup<Task>;

const core = globalThis.WerkbordPlanning;
export const { daysBetween, isDay, spanOf, PX_PER_DAY, ROW_HEIGHT, viewRange, ticks, barFor, barsFor,
  firstRows, arrows, warningsByTask, worst, spanTitle, localToday } = core;

export function groupTasks(tasks: readonly Task[], labels: readonly Label[], by: 'none' | 'label' | 'status' | 'mode'): Group[] {
  return core.groupTasks(tasks, labels, by, {
    statusOf: (t) => t.state,
    statusTitles: { backlog: 'Backlog', doing: 'In progress', review: 'Review', done: 'Done' },
    statusOrder: ['backlog', 'doing', 'review', 'done'],
    positionOf: (t) => t.position,
  });
}

export function dependencyMap(tasks: readonly Task[]): Map<string, readonly string[]> {
  const out = new Map<string, readonly string[]>();
  for (const t of tasks) if (t.orchestration?.dependencies?.length) out.set(t.id, t.orchestration.dependencies);
  return out;
}
