// The timeline's arithmetic and layout, on wall dates ("2026-10-05") so no time zone is ever applied:
// planned work is in whole days. No framework code, so it can be tested on its own. What is wrong with
// the dependencies is worked out by the controller (internal/planning); this only places things.

import { addDays } from './calendar';
import { labelsOf, modeLabel, modeOf } from './labels';
import type { Label, Task, TaskState, TimelineWarning } from './types';

const DAY = 86_400_000;
const at = (d: string) => Date.parse(`${d}T12:00:00Z`);

/** Whole days from a to b (negative if b is earlier). */
export function daysBetween(a: string, b: string): number {
  return Math.round((at(b) - at(a)) / DAY);
}

export const isDay = (s: string | undefined): s is string => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s) && Number.isFinite(at(s));

export interface Span {
  /** First day, inclusive. */
  start: string;
  /** Last day, inclusive. Equal to `start` for a one-day item and for a milestone. */
  end: string;
  milestone: boolean;
}

/**
 * Where a task sits in time, or null if it has no usable dates. Only a start is a one-day item, only an
 * end is something due that day, a milestone is its single date. Bad dates are ignored here and
 * reported by the controller's warnings.
 */
export function spanOf(task: Pick<Task, 'plan'>): Span | null {
  const p = task.plan;
  if (!p) return null;
  const start = isDay(p.start) ? p.start : undefined;
  const end = isDay(p.end) ? p.end : undefined;
  const first = start ?? end;
  if (!first) return null;
  if (p.milestone) return { start: first, end: first, milestone: true };
  const last = end ?? first;
  if (daysBetween(first, last) < 0) return null; // end before start: shown nowhere, warned about
  return { start: first, end: last, milestone: false };
}

export type Zoom = 'week' | 'month' | 'quarter';

/** Pixels per day at each zoom: a day is wide enough to click at week zoom, and a quarter fits a screen. */
export const PX_PER_DAY: Record<Zoom, number> = { week: 40, month: 16, quarter: 6 };

export interface Range {
  start: string;
  end: string;
}

/** The days to draw: everything dated, and today, with some air, always at least `min` days. */
export function viewRange(spans: readonly Span[], today: string, pad = 7, min = 28): Range {
  let start = today;
  let end = today;
  for (const s of spans) {
    if (daysBetween(s.start, start) > 0) start = s.start;
    if (daysBetween(end, s.end) > 0) end = s.end;
  }
  start = addDays(start, -pad);
  end = addDays(end, pad);
  const short = min - (daysBetween(start, end) + 1);
  if (short > 0) end = addDays(end, short);
  return { start, end };
}

export interface Tick {
  day: string;
  x: number;
  label: string;
  /** A larger boundary (a month, at week and month zoom; a quarter's months otherwise). */
  major: boolean;
}

const MONTH = new Intl.DateTimeFormat('en', { month: 'short', timeZone: 'UTC' });

/** Header ticks: days at week zoom, Mondays at month zoom, month starts at quarter zoom. */
export function ticks(range: Range, zoom: Zoom): Tick[] {
  const out: Tick[] = [];
  const total = daysBetween(range.start, range.end) + 1;
  const px = PX_PER_DAY[zoom];
  for (let i = 0; i < total; i++) {
    const day = addDays(range.start, i);
    const dom = Number(day.slice(8));
    const monday = new Date(at(day)).getUTCDay() === 1;
    if (zoom === 'week') out.push({ day, x: i * px, label: dom === 1 ? `${MONTH.format(new Date(at(day)))} 1` : String(dom), major: dom === 1 || monday });
    else if (zoom === 'month' && monday) out.push({ day, x: i * px, label: dom === 1 ? MONTH.format(new Date(at(day))) : String(dom), major: dom <= 7 });
    else if (zoom === 'quarter' && dom === 1) out.push({ day, x: i * px, label: MONTH.format(new Date(at(day))), major: true });
  }
  return out;
}

export interface Bar {
  taskId: string;
  x: number;
  width: number;
  milestone: boolean;
  span: Span;
}

/** The bar for a span: from the left edge of its first day to the right edge of its last. A milestone is one day wide. */
export function barFor(taskId: string, span: Span, range: Range, zoom: Zoom): Bar {
  const px = PX_PER_DAY[zoom];
  const x = daysBetween(range.start, span.start) * px;
  return { taskId, x, width: (daysBetween(span.start, span.end) + 1) * px, milestone: span.milestone, span };
}

export type GroupBy = 'none' | 'label' | 'status' | 'mode';

export interface Group {
  key: string;
  title: string;
  /** A label's colour, for a group made from a label. */
  color?: string;
  tasks: Task[];
}

const STATE_ORDER: TaskState[] = ['backlog', 'doing', 'review', 'done'];
const STATE_TITLE: Record<TaskState, string> = { backlog: 'Backlog', doing: 'Doing', review: 'Review', done: 'Done' };

/**
 * Groups tasks for the timeline's rows. By label, a task with several labels is listed under each (the
 * same work, found wherever it is looked for) and one with none under "No label". Within a group the
 * earliest-starting work comes first and undated work last.
 */
export function groupTasks(tasks: readonly Task[], labels: readonly Label[], by: GroupBy): Group[] {
  const order = (a: Task, b: Task) => {
    const sa = spanOf(a);
    const sb = spanOf(b);
    if (sa && sb) return daysBetween(sb.start, sa.start) || daysBetween(sb.end, sa.end) || a.position - b.position;
    if (sa) return -1;
    if (sb) return 1;
    return a.position - b.position;
  };
  const sorted = [...tasks].sort(order);
  if (by === 'none') return [{ key: 'all', title: 'All work', tasks: sorted }];

  const groups = new Map<string, Group>();
  const put = (key: string, title: string, task: Task, color?: string) => {
    let g = groups.get(key);
    if (!g) groups.set(key, (g = { key, title, color, tasks: [] }));
    g.tasks.push(task);
  };
  for (const t of sorted) {
    if (by === 'status') put(t.state, STATE_TITLE[t.state] ?? t.state, t);
    else if (by === 'mode') put(modeOf(t), modeLabel(modeOf(t)), t);
    else {
      const ls = labelsOf(t, labels);
      if (!ls.length) put('~none', 'No label', t);
      for (const l of ls) put(l.id, l.name, t, l.color);
    }
  }
  const list = [...groups.values()];
  if (by === 'status') list.sort((a, b) => STATE_ORDER.indexOf(a.key as TaskState) - STATE_ORDER.indexOf(b.key as TaskState));
  else if (by === 'mode') list.sort((a, b) => ['human', 'agent', 'hybrid'].indexOf(a.key) - ['human', 'agent', 'hybrid'].indexOf(b.key));
  else list.sort((a, b) => (a.key === '~none' ? 1 : b.key === '~none' ? -1 : a.title.localeCompare(b.title, undefined, { sensitivity: 'base' })));
  return list;
}

export interface Arrow {
  /** The task that must finish first, and the one that waits for it. */
  from: string;
  to: string;
  /** SVG path from the right edge of the first bar to the left edge of the second. */
  d: string;
  /** The dependent starts before its dependency ends: drawn as a conflict. */
  conflict: boolean;
}

export const ROW_HEIGHT = 36;

/**
 * Arrows for dependencies where both ends are on the chart. `rows` says which row each task is drawn
 * in (a task shown under several groups gets an arrow to each of its rows’ first appearance only).
 */
export function arrows(bars: ReadonlyMap<string, Bar>, rowOf: ReadonlyMap<string, number>, deps: ReadonlyMap<string, readonly string[]>): Arrow[] {
  const out: Arrow[] = [];
  for (const [to, froms] of deps) {
    const b = bars.get(to);
    const rb = rowOf.get(to);
    if (!b || rb === undefined) continue;
    for (const from of froms) {
      const a = bars.get(from);
      const ra = rowOf.get(from);
      if (!a || ra === undefined) continue;
      const x1 = a.x + a.width;
      const y1 = ra * ROW_HEIGHT + ROW_HEIGHT / 2;
      const x2 = b.x;
      const y2 = rb * ROW_HEIGHT + ROW_HEIGHT / 2;
      const mid = x1 + 8;
      const d = x2 >= x1 + 16 ? `M${x1} ${y1} H${mid} V${y2} H${x2}` : `M${x1} ${y1} H${x1 + 8} V${(y1 + y2) / 2} H${x2 - 8} V${y2} H${x2}`;
      out.push({ from, to, d, conflict: daysBetween(b.span.start, a.span.end) > 0 });
    }
  }
  return out;
}

/** Warnings by the task they are about, for marking bars and rows. */
export function warningsByTask(warnings: readonly TimelineWarning[]): Map<string, TimelineWarning[]> {
  const out = new Map<string, TimelineWarning[]>();
  for (const w of warnings) {
    const ids = new Set([w.itemId, ...(w.cycle ?? [])]);
    for (const id of ids) out.set(id, [...(out.get(id) ?? []), w]);
  }
  return out;
}

export function worst(ws: readonly TimelineWarning[] | undefined): 'error' | 'warning' | 'info' | '' {
  if (!ws?.length) return '';
  return ws.some((w) => w.severity === 'error') ? 'error' : ws.some((w) => w.severity === 'warning') ? 'warning' : 'info';
}

/** "Oct 5 – 9", "Oct 28 – Nov 2", "Oct 5" for a day, "Oct 5, 2027" when the year is not this one. */
export function spanTitle(span: Span, thisYear: number): string {
  const f = (d: string, year: boolean) => new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: year ? 'numeric' : undefined, timeZone: 'UTC' }).format(new Date(at(d)));
  const y = (d: string) => Number(d.slice(0, 4)) !== thisYear;
  if (span.start === span.end) return f(span.start, y(span.start));
  const sameMonth = span.start.slice(0, 7) === span.end.slice(0, 7);
  const tail = sameMonth ? String(Number(span.end.slice(8))) : f(span.end, false);
  return `${f(span.start, false)} – ${tail}${y(span.end) ? `, ${span.end.slice(0, 4)}` : ''}`;
}

/** Today's date on this computer's calendar, as a wall date. */
export function localToday(now: number): string {
  const d = new Date(now);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** The bar of every task that has dates, by task ID. */
export function barsFor(tasks: readonly Task[], range: Range, zoom: Zoom): Map<string, Bar> {
  const out = new Map<string, Bar>();
  for (const t of tasks) {
    const s = spanOf(t);
    if (s) out.set(t.id, barFor(t.id, s, range, zoom));
  }
  return out;
}

/** The first row each task is drawn in, by task ID (a task listed under several groups has several rows). */
export function firstRows(keys: readonly (string | null)[]): Map<string, number> {
  const out = new Map<string, number>();
  keys.forEach((id, i) => {
    if (id !== null && !out.has(id)) out.set(id, i);
  });
  return out;
}

/** What each task waits for, by task ID, for the tasks that wait for anything. */
export function dependencyMap(tasks: readonly Task[]): Map<string, readonly string[]> {
  const out = new Map<string, readonly string[]>();
  for (const t of tasks) if (t.orchestration?.dependencies?.length) out.set(t.id, t.orchestration.dependencies);
  return out;
}
