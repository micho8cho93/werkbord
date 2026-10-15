// What the phone's Calendar shows, from the tasks every project already holds. A task has two kinds of time and they are kept
// apart on purpose (DESIGN.md: *plan* is the dates of work, *schedule* is when an agent starts): a planned span is whole days and is
// drawn as a bar; a scheduled start is an instant and is drawn as a marker at its hour. Nothing here is invented or inferred: a task with
// neither has no place in time and is not shown. No framework code, so it can be tested on its own.

import { addDays, weekStart } from '../calendar';
import { daysBetween, localToday, spanOf } from '../timeline';
import type { Task, TaskState } from '../types';

export type CalKind = 'planned' | 'scheduled';

export interface CalItem {
  /** Unique per task and kind: one task can be both planned and scheduled. */
  key: string;
  taskId: string;
  projectId: string;
  projectName: string;
  title: string;
  kind: CalKind;
  /** First and last day, on this device's calendar, inclusive. A scheduled start is one day. */
  start: string;
  end: string;
  /** Minutes after local midnight of a scheduled start; absent for a planned span. */
  minute?: number;
  /** The scheduled instant, as the controller holds it. */
  at?: string;
  state: TaskState;
  milestone: boolean;
}

const p2 = (n: number) => String(n).padStart(2, '0');

/** An instant's day on this device's calendar, and how far into it. */
export function localParts(at: string): { day: string; minute: number } | null {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return null;
  return { day: `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`, minute: d.getHours() * 60 + d.getMinutes() };
}

/** Every dated or scheduled task of the given projects, planned spans and scheduled starts as separate items. */
export function calendarItems(tasks: readonly Task[], projectName: (id: string) => string): CalItem[] {
  const out: CalItem[] = [];
  for (const t of tasks) {
    if (t.archivedAt) continue;
    const base = { taskId: t.id, projectId: t.projectId, projectName: projectName(t.projectId), title: t.title, state: t.state };
    const span = spanOf(t);
    if (span) out.push({ ...base, key: `${t.id}/plan`, kind: 'planned', start: span.start, end: span.end, milestone: span.milestone });
    const o = t.orchestration;
    const when = o?.enabled && o.scheduledAt ? localParts(o.scheduledAt) : null;
    if (when) out.push({ ...base, key: `${t.id}/schedule`, kind: 'scheduled', start: when.day, end: when.day, minute: when.minute, at: o!.scheduledAt, milestone: false });
  }
  return out.sort((a, b) => a.start.localeCompare(b.start) || (a.minute ?? -1) - (b.minute ?? -1) || a.title.localeCompare(b.title));
}

/** The items that touch a day. */
export function itemsOn(items: readonly CalItem[], day: string): CalItem[] {
  return items.filter((i) => i.start <= day && i.end >= day);
}

/** What a day holds, for the small marks on a month's cells. */
export function marks(items: readonly CalItem[], day: string): { planned: number; scheduled: number; milestone: number } {
  const on = itemsOn(items, day);
  return {
    planned: on.filter((i) => i.kind === 'planned' && !i.milestone).length,
    scheduled: on.filter((i) => i.kind === 'scheduled').length,
    milestone: on.filter((i) => i.milestone).length,
  };
}

/** The first day of the month a day is in. */
export const monthStart = (day: string): string => `${day.slice(0, 7)}-01`;

/** The month before or after the one a day is in, as its first day. */
export function shiftMonth(day: string, by: number): string {
  const y = Number(day.slice(0, 4));
  const m = Number(day.slice(5, 7)) - 1 + by;
  return `${y + Math.floor(m / 12)}-${p2((((m % 12) + 12) % 12) + 1)}-01`;
}

/** The weeks a month's grid shows, Monday first: whole weeks from the one the 1st is in to the one its last day is in. */
export function monthGrid(day: string): string[][] {
  const first = monthStart(day);
  const last = addDays(shiftMonth(first, 1), -1);
  const weeks: string[][] = [];
  for (let w = weekStart(first); w <= last; w = addDays(w, 7)) weeks.push(Array.from({ length: 7 }, (_, i) => addDays(w, i)));
  return weeks;
}

export function monthTitle(day: string): string {
  return new Intl.DateTimeFormat('en', { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(new Date(`${monthStart(day)}T12:00:00Z`));
}

export function dayTitle(day: string): string {
  return new Intl.DateTimeFormat('en', { weekday: 'long', month: 'long', day: 'numeric', timeZone: 'UTC' }).format(new Date(`${day}T12:00:00Z`));
}

/** The seven days of the week a day is in, Monday first. */
export const weekDays = (day: string): string[] => Array.from({ length: 7 }, (_, i) => addDays(weekStart(day), i));

/** A day's items split the way the week view draws them: whole-day work on top, scheduled starts at their hour. */
export function dayLayout(items: readonly CalItem[], day: string): { allDay: CalItem[]; timed: CalItem[] } {
  const on = itemsOn(items, day);
  return { allDay: on.filter((i) => i.kind === 'planned'), timed: on.filter((i) => i.kind === 'scheduled') };
}

/** Where today is relative to a day, for the words on a row ("today", "in 3 days", "2 days ago"). */
export function relativeDay(day: string, now: number): string {
  const n = daysBetween(localToday(now), day);
  if (n === 0) return 'today';
  if (n === 1) return 'tomorrow';
  if (n === -1) return 'yesterday';
  return n > 0 ? `in ${n} days` : `${-n} days ago`;
}
