import { describe, expect, it } from 'vitest';
import { ARCHIVE_AFTER, daysLeft, erasure, TRASH_DAYS } from './trash';
import { ProjectScope } from './scope.svelte';
import type { Task } from './types';

const T0 = '2026-11-03T09:00:00.000Z';
const at = (days: number) => Date.parse(T0) + days * 86_400_000;

describe('how long a deleted thing is kept', () => {
  it('counts whole days left, and says it in words', () => {
    expect(daysLeft(T0, at(0))).toBe(TRASH_DAYS);
    expect(daysLeft(T0, at(10.5))).toBe(20);
    expect(daysLeft(T0, at(TRASH_DAYS + 3))).toBe(0);
    expect(erasure(T0, at(0))).toBe('Erased for good in 30 days');
    expect(erasure(T0, at(TRASH_DAYS - 1))).toBe('Erased tomorrow');
    expect(erasure(T0, at(TRASH_DAYS))).toBe('Erased today');
    expect(daysLeft('not a date', at(0))).toBe(0);
  });
  it('offers the periods the controller accepts', () => {
    expect(ARCHIVE_AFTER.map((a) => a.days)).toEqual([0, 7, 14, 30, 90]);
  });
});

describe('the archive and the trash of a project', () => {
  const task = (id: string, extra: Partial<Task> = {}): Task => ({ id, projectId: 'prj_a', title: id, description: '', state: 'done', version: 1, position: 1, createdAt: T0, updatedAt: T0, ...extra } as Task);
  it('keeps a deleted task out of the archive and out of the active list, and drops an erased one with its run', () => {
    const scope = new ProjectScope('prj_a');
    scope.tasks = [task('open', { state: 'backlog' }), task('archived', { archivedAt: T0 }), task('deleted', { archivedAt: T0, deletedAt: T0 })];
    expect(scope.activeTasks.map((t) => t.id)).toEqual(['open']);
    expect(scope.archivedTasks.map((t) => t.id)).toEqual(['archived']);
    expect(scope.deletedTasks.map((t) => t.id)).toEqual(['deleted']);
    expect(scope.apply({ seq: 9, type: 'task.deleted', projectId: 'prj_a', taskId: 'deleted', payload: { id: 'deleted' }, createdAt: T0 } as never)).toBe(true);
    expect(scope.tasks.map((t) => t.id)).toEqual(['open', 'archived']);
    // Another project's erasure is not this one's business.
    expect(scope.apply({ seq: 10, type: 'task.deleted', projectId: 'prj_b', payload: { id: 'open' }, createdAt: T0 } as never)).toBe(false);
    expect(scope.tasks).toHaveLength(2);
  });
});
