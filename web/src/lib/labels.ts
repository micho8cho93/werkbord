// Labels and work modes: what they look like and how tasks are filtered by them. No framework code,
// so it can be tested on its own. The controller owns the rules (names, colours, uniqueness); this
// is only presentation and the same filter the API offers.

import type { Label, Task, WorkMode } from './types';

/** The three answers to "who does this?". Fixed, unlike labels, which are whatever the person wants. */
export const WORK_MODES: readonly { id: WorkMode; label: string; hint: string }[] = [
  { id: 'human', label: 'Human', hint: 'A person does it. No agent is ever started on it.' },
  { id: 'agent', label: 'Agent', hint: 'An agent does it, started by hand or on a schedule.' },
  { id: 'hybrid', label: 'Hybrid', hint: 'An agent may start on it and a person finishes or checks it.' },
];

/** A task's mode, reading an absent one as it always was: agent work. */
export function modeOf(task: Pick<Task, 'workMode'>): WorkMode {
  return task.workMode ?? 'agent';
}

export function modeLabel(mode: WorkMode): string {
  return WORK_MODES.find((m) => m.id === mode)?.label ?? mode;
}

/** Colours offered when making a label. Only colours: the names are the person's. */
export const LABEL_COLORS: readonly string[] = ['#d14d41', '#da702c', '#d0a215', '#879a39', '#3aa99f', '#4385be', '#8b7ec8', '#ce5d97', '#6f6e69'];

/** A text colour that reads on `bg` (#rrggbb): the one with the better contrast. */
export function inkFor(bg: string): '#ffffff' | '#14130f' {
  const m = /^#([0-9a-f]{6})$/i.exec(bg);
  if (!m) return '#14130f';
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(m[1].slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  const lum = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  // White text on bg has contrast (1.05)/(lum+0.05); dark text has (lum+0.05)/(0.0065+0.05).
  return 1.05 / (lum + 0.05) >= (lum + 0.05) / 0.0565 ? '#ffffff' : '#14130f';
}

/** The labels of a task, in the order they were put on, leaving out any this copy does not know yet. */
export function labelsOf(task: Pick<Task, 'labelIds'>, labels: readonly Label[]): Label[] {
  const byId = new Map(labels.map((l) => [l.id, l]));
  return (task.labelIds ?? []).flatMap((id) => {
    const l = byId.get(id);
    return l ? [l] : [];
  });
}

/** Labels by name, the way the controller lists them. */
export function sortedLabels<T extends Label>(labels: readonly T[]): T[] {
  return [...labels].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id.localeCompare(b.id));
}

export interface TaskFilter {
  /** Label IDs to keep. */
  labels: string[];
  /** Keep tasks with any of the labels, or only those with all of them. */
  match: 'any' | 'all';
  /** Keep one work mode, or all of them when empty. */
  mode: WorkMode | '';
}

export const noFilter = (): TaskFilter => ({ labels: [], match: 'any', mode: '' });

export function filterActive(f: TaskFilter): boolean {
  return f.labels.length > 0 || f.mode !== '';
}

/** The same rule as the controller's TaskFilter: every part that is set must hold. */
export function matches(task: Pick<Task, 'labelIds' | 'workMode'>, f: TaskFilter): boolean {
  if (f.mode && modeOf(task) !== f.mode) return false;
  if (!f.labels.length) return true;
  const have = new Set(task.labelIds ?? []);
  return f.match === 'all' ? f.labels.every((id) => have.has(id)) : f.labels.some((id) => have.has(id));
}

export function toggleLabel(f: TaskFilter, id: string): TaskFilter {
  return { ...f, labels: f.labels.includes(id) ? f.labels.filter((x) => x !== id) : [...f.labels, id] };
}

/** Drops labels from a filter that no longer exist (deleted elsewhere), so it cannot hide everything. */
export function pruneFilter(f: TaskFilter, labels: readonly Label[]): TaskFilter {
  const known = new Set(labels.map((l) => l.id));
  const kept = f.labels.filter((id) => known.has(id));
  return kept.length === f.labels.length ? f : { ...f, labels: kept };
}
