// The label and work-mode filter of each project, shared by its Board and its Timeline: narrow the
// board to "Design" work and the timeline shows the same. Kept for this page only; a filter is a way
// of looking, not something the controller stores.

import { SvelteMap } from 'svelte/reactivity';
import { noFilter, pruneFilter, type TaskFilter } from './labels';
import type { Label } from './types';

const filters = new SvelteMap<string, TaskFilter>();

export function filterFor(projectId: string): TaskFilter {
  return filters.get(projectId) ?? noFilter();
}

export function setFilter(projectId: string, f: TaskFilter): void {
  filters.set(projectId, f);
}

/** Forgets labels that were deleted, so a stale filter cannot hide every task. */
export function pruneFor(projectId: string, labels: readonly Label[]): void {
  const f = filters.get(projectId);
  if (!f) return;
  const pruned = pruneFilter(f, labels);
  if (pruned !== f) filters.set(projectId, pruned);
}
