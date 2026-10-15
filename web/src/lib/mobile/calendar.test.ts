import { describe, expect, it } from 'vitest';
import { calendarItems, dayLayout, itemsOn, marks, monthGrid, monthStart, relativeDay, shiftMonth, weekDays } from './calendar';
import type { Task } from '../types';

const task = (over: Partial<Task>): Task =>
  ({ id: 't', projectId: 'p', title: 'T', description: '', state: 'backlog', position: 0, execution: {}, version: 1, createdAt: '', updatedAt: '', ...over }) as Task;

describe('calendarItems', () => {
  it('keeps a planned span and a scheduled start apart, even on one task', () => {
    const items = calendarItems(
      [task({ id: 'a', plan: { start: '2026-10-05', end: '2026-10-07' }, orchestration: { enabled: true, scheduledAt: '2026-10-06T09:30:00', dependencies: [], expectedPaths: [] } })],
      () => 'Fixture',
    );
    expect(items.map((i) => [i.kind, i.start, i.end])).toEqual([
      ['planned', '2026-10-05', '2026-10-07'],
      ['scheduled', '2026-10-06', '2026-10-06'],
    ]);
    expect(items[1].minute).toBe(9 * 60 + 30);
    expect(items[0].minute).toBeUndefined();
    expect(items[0].key).not.toBe(items[1].key);
    expect(items[0].projectName).toBe('Fixture');
  });

  it('shows nothing for a task with no dates, an archived one, or a schedule that is off', () => {
    const items = calendarItems(
      [
        task({ id: 'none' }),
        task({ id: 'old', archivedAt: 'x', plan: { start: '2026-10-05' } }),
        task({ id: 'off', orchestration: { enabled: false, scheduledAt: '2026-10-06T09:30:00', dependencies: [], expectedPaths: [] } }),
        task({ id: 'bad', plan: { start: '2026-10-09', end: '2026-10-01' } }),
      ],
      () => '',
    );
    expect(items).toEqual([]);
  });

  it('marks a milestone and puts a single date on one day', () => {
    const [m] = calendarItems([task({ plan: { end: '2026-10-12', milestone: true } })], () => '');
    expect(m).toMatchObject({ milestone: true, start: '2026-10-12', end: '2026-10-12' });
  });

  it('sorts by day, then time', () => {
    const items = calendarItems(
      [
        task({ id: 'b', title: 'B', orchestration: { enabled: true, scheduledAt: '2026-10-06T15:00:00', dependencies: [], expectedPaths: [] } }),
        task({ id: 'a', title: 'A', orchestration: { enabled: true, scheduledAt: '2026-10-06T08:00:00', dependencies: [], expectedPaths: [] } }),
        task({ id: 'c', title: 'C', plan: { start: '2026-10-01' } }),
      ],
      () => '',
    );
    expect(items.map((i) => i.taskId)).toEqual(['c', 'a', 'b']);
  });
});

describe('itemsOn and marks', () => {
  const items = calendarItems(
    [
      task({ id: 'a', plan: { start: '2026-10-05', end: '2026-10-07' } }),
      task({ id: 'b', plan: { end: '2026-10-06', milestone: true } }),
      task({ id: 'c', orchestration: { enabled: true, scheduledAt: '2026-10-06T09:00:00', dependencies: [], expectedPaths: [] } }),
    ],
    () => '',
  );
  it('finds what touches a day', () => {
    expect(itemsOn(items, '2026-10-05').map((i) => i.taskId)).toEqual(['a']);
    expect(itemsOn(items, '2026-10-06').map((i) => i.taskId).sort()).toEqual(['a', 'b', 'c']);
    expect(itemsOn(items, '2026-10-08')).toEqual([]);
  });
  it('counts each kind separately', () => {
    expect(marks(items, '2026-10-06')).toEqual({ planned: 1, scheduled: 1, milestone: 1 });
  });
  it('splits a day into whole-day work and timed starts', () => {
    const { allDay, timed } = dayLayout(items, '2026-10-06');
    expect(allDay.map((i) => i.taskId).sort()).toEqual(['a', 'b']);
    expect(timed.map((i) => i.taskId)).toEqual(['c']);
  });
});

describe('the month grid', () => {
  it('runs whole weeks from Monday', () => {
    const grid = monthGrid('2026-10-15');
    expect(grid[0][0]).toBe('2026-09-28');
    expect(grid[0][3]).toBe('2026-10-01');
    expect(grid[grid.length - 1][6]).toBe('2026-11-01');
    expect(grid.every((w) => w.length === 7)).toBe(true);
    expect(grid).toHaveLength(5);
  });
  it('moves between months across a year', () => {
    expect(shiftMonth('2026-12-20', 1)).toBe('2027-01-01');
    expect(shiftMonth('2026-01-20', -1)).toBe('2025-12-01');
    expect(shiftMonth('2026-10-15', 0)).toBe('2026-10-01');
    expect(monthStart('2026-10-15')).toBe('2026-10-01');
  });
  it('lists a week from Monday', () => {
    expect(weekDays('2026-10-15')).toEqual(['2026-10-12', '2026-10-13', '2026-10-14', '2026-10-15', '2026-10-16', '2026-10-17', '2026-10-18']);
  });
});

describe('relativeDay', () => {
  const now = new Date(2026, 9, 15, 12).getTime();
  it('says it in words', () => {
    expect(relativeDay('2026-10-15', now)).toBe('today');
    expect(relativeDay('2026-10-16', now)).toBe('tomorrow');
    expect(relativeDay('2026-10-14', now)).toBe('yesterday');
    expect(relativeDay('2026-10-20', now)).toBe('in 5 days');
    expect(relativeDay('2026-10-10', now)).toBe('5 days ago');
  });
});
