// My Work, from the tasks every project holds: what is the person's to do, what is going on, what is coming up and what changed lately.
// In Individual every task is the person's own; "yours" is the work that is theirs to do rather than an agent's. No framework code,
// so it can be tested on its own.

import { modeOf } from '../labels';
import { calendarItems, relativeDay, type CalItem } from './calendar';
import { localToday } from '../timeline';
import type { Task } from '../types';

export type WorkTab = 'yours' | 'active' | 'upcoming' | 'recent';

export interface WorkGroups {
  yours: Task[];
  active: Task[];
  upcoming: { task: Task; item: CalItem }[];
  recent: Task[];
}

const byUpdated = (a: Task, b: Task) => b.updatedAt.localeCompare(a.updatedAt);
const RECENT = 15;
/** How far ahead "upcoming" looks. Beyond it the Calendar is the place. */
export const UPCOMING_DAYS = 14;

/**
 * The four lists. A task can be in more than one: a person's own task in progress is both yours and active. Done and archived work is
 * only ever in Recent.
 */
export function workGroups(tasks: readonly Task[], now: number): WorkGroups {
  const live = tasks.filter((t) => !t.archivedAt);
  const open = live.filter((t) => t.state !== 'done');
  const today = localToday(now);
  const horizon = localToday(now + UPCOMING_DAYS * 86_400_000);

  const rank = (t: Task) => (t.state === 'doing' ? 0 : t.state === 'review' ? 1 : 2);
  const yours = open.filter((t) => modeOf(t) !== 'agent').sort((a, b) => rank(a) - rank(b) || byUpdated(a, b));
  const active = open.filter((t) => t.state === 'doing').sort(byUpdated);

  const byTask = new Map(open.map((t) => [t.id, t]));
  const upcoming = calendarItems(open, () => '')
    // Ongoing or still to come, and within the next fortnight; the first of a task's two kinds of time is the one listed.
    .filter((i) => i.end >= today && i.start <= horizon)
    .filter((i, n, all) => all.findIndex((x) => x.taskId === i.taskId) === n)
    .map((item) => ({ task: byTask.get(item.taskId)!, item }));

  const recent = [...live].sort(byUpdated).slice(0, RECENT);
  return { yours, active, upcoming, recent };
}

/** What an upcoming row says about when: that it is under way, or how soon it starts. */
export function upcomingNote(item: CalItem, now: number): string {
  return item.start <= localToday(now) ? 'under way' : `starts ${relativeDay(item.start, now)}`;
}
