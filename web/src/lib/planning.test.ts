import { describe, expect, it } from 'vitest';
import { validatePlanDraft, validatePlanRange } from './planning';
import type { Task } from './types';

function task(id: string, plan: Task['plan'] = {}, dependencies: string[] = [], extras: Partial<Task> = {}): Task {
  return {
    id, projectId: 'p', title: id, description: '', state: 'backlog', position: 0, execution: {}, version: 1,
    createdAt: '', updatedAt: '', plan, orchestration: { enabled: false, dependencies, expectedPaths: [] }, ...extras,
  };
}

describe('planning draft validation', () => {
  it('rejects a reversed date range and a milestone without a date', () => {
    expect(validatePlanRange({ start: '2026-10-10', end: '2026-10-09' }, 'Build')[0]).toMatchObject({ field: 'plan.end', code: 'invalid_range' });
    expect(validatePlanRange({ milestone: true }, 'Release')[0]).toMatchObject({ field: 'plan.start', code: 'milestone_without_date' });
  });

  it('allows a dependent to start on its prerequisite end date and leaves undated work advisory', () => {
    const prerequisite = task('pre', { start: '2026-10-05', end: '2026-10-09' });
    const sameDay = task('next', { start: '2026-10-09', end: '2026-10-12' }, ['pre']);
    const undated = task('unscheduled');
    const advisory = task('advisory', { start: '2026-10-10' }, ['unscheduled']);
    expect(validatePlanDraft([prerequisite, sameDay], sameDay)).toEqual([]);
    expect(validatePlanDraft([undated, advisory], advisory)).toEqual([]);
  });

  it('checks direct dependants when an edited prerequisite moves later', () => {
    const prerequisite = task('pre', { start: '2026-10-05', end: '2026-10-09' });
    const dependent = task('next', { start: '2026-10-08' }, ['pre']);
    expect(validatePlanDraft([prerequisite, dependent], task('pre', { start: '2026-10-05', end: '2026-10-10' }))).toMatchObject([
      { itemId: 'next', otherId: 'pre', field: 'plan.end', code: 'starts_before_dependency_ends' },
    ]);
  });

  it('rejects a newly selected unfinished archived prerequisite and a new cycle', () => {
    const archived = task('archived', {}, [], { archivedAt: '2026-10-01T00:00:00Z' });
    const current = task('current');
    expect(validatePlanDraft([archived, current], task('current', {}, ['archived']))).toMatchObject([
      { field: 'dependencies', code: 'archived_dependency' },
    ]);

    const a = task('a', {}, ['b']);
    const b = task('b');
    expect(validatePlanDraft([a, b], task('b', {}, ['a']))).toMatchObject([
      { field: 'dependencies', code: 'dependency_cycle' },
    ]);
  });

  it('does not force repair of an unrelated historical conflict', () => {
    const prerequisite = task('pre', { start: '2026-10-05', end: '2026-10-09' });
    const historical = task('historical', { start: '2026-10-08' }, ['pre']);
    const unrelated = task('other');
    expect(validatePlanDraft([prerequisite, historical, unrelated], task('other', { start: '2026-10-20' }))).toEqual([]);
  });
});
