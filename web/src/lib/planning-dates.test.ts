import { describe, expect, it } from 'vitest';
import { daysBetween, localToday, spanOf, spanTitle } from './planning-dates';
import type { Task } from './types';

const task = (id: string, plan?: Task['plan'], extra: Partial<Task> = {}): Task =>
  ({ id, projectId: 'p', title: id, description: '', state: 'backlog', position: 1, execution: {}, version: 1, createdAt: '', updatedAt: '', plan, ...extra }) as Task;

describe('spans', () => {
  it('reads a plan as inclusive whole days', () => {
    expect(spanOf(task('a', { start: '2026-10-05', end: '2026-10-09' }))).toEqual({ start: '2026-10-05', end: '2026-10-09', milestone: false });
    expect(spanOf(task('a', { start: '2026-10-05' }))).toEqual({ start: '2026-10-05', end: '2026-10-05', milestone: false });
    expect(spanOf(task('a', { end: '2026-10-09' }))).toEqual({ start: '2026-10-09', end: '2026-10-09', milestone: false });
    expect(spanOf(task('a', { start: '2026-10-12', milestone: true }))).toEqual({ start: '2026-10-12', end: '2026-10-12', milestone: true });
  });

  it('places nothing it cannot read', () => {
    expect(spanOf(task('a'))).toBeNull();
    expect(spanOf(task('a', {}))).toBeNull();
    expect(spanOf(task('a', { start: 'soon' }))).toBeNull();
    expect(spanOf(task('a', { start: '2026-10-09', end: '2026-10-05' }))).toBeNull();
  });

  it('counts days across month and year ends', () => {
    expect(daysBetween('2026-10-05', '2026-10-09')).toBe(4);
    expect(daysBetween('2026-12-30', '2027-01-02')).toBe(3);
    expect(daysBetween('2026-10-09', '2026-10-05')).toBe(-4);
  });

  it('titles spans briefly', () => {
    expect(spanTitle({ start: '2026-10-05', end: '2026-10-09', milestone: false }, 2026)).toBe('Oct 5 – 9');
    expect(spanTitle({ start: '2026-10-28', end: '2026-11-02', milestone: false }, 2026)).toBe('Oct 28 – Nov 2');
    expect(spanTitle({ start: '2026-10-05', end: '2026-10-05', milestone: true }, 2026)).toBe('Oct 5');
    expect(spanTitle({ start: '2027-01-05', end: '2027-01-05', milestone: false }, 2026)).toBe('Jan 5, 2027');
  });
});

describe('today', () => {
  it('is the date on this computer’s calendar', () => {
    expect(localToday(new Date(2026, 9, 5, 23, 59).getTime())).toBe('2026-10-05');
    expect(localToday(new Date(2026, 0, 1, 0, 0).getTime())).toBe('2026-01-01');
  });
});
