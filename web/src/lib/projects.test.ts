import { describe, expect, it } from 'vitest';
import { activitySummary, attentionCount, filterProjects, projectColor, projectHue, projectInitial, stepIndex } from './projects';
import type { Project, ProjectActivity } from './types';

const project = (id: string, name: string, repoPath = `/code/${name}`): Project => ({
  id, name, repoPath, execution: {}, createdAt: '2026-10-04T10:00:00Z', updatedAt: '2026-10-04T10:00:00Z',
});

describe('telling projects apart', () => {
  it('gives a project the same colour every time, and different projects different ones', () => {
    expect(projectColor('prj_abc')).toBe(projectColor('prj_abc'));
    const hues = new Set(['prj_a1', 'prj_b2', 'prj_c3', 'prj_d4', 'prj_e5', 'prj_f6'].map(projectHue));
    expect(hues.size).toBeGreaterThanOrEqual(5);
    for (const h of hues) expect(h).toBeGreaterThanOrEqual(0);
    for (const h of hues) expect(h).toBeLessThan(360);
  });

  it('shows one letter', () => {
    expect(projectInitial('devboard')).toBe('D');
    expect(projectInitial('  my-app')).toBe('M');
    expect(projectInitial('')).toBe('?');
    expect(projectInitial('😀 fun')).toBe('😀');
  });
});

describe('finding a project quickly', () => {
  const ps = [project('1', 'Website', '/code/site'), project('2', 'Webhooks'), project('3', 'API gateway', '/code/web-api'), project('4', 'Docs')];

  it('lists everything for an empty search, in order', () => {
    expect(filterProjects(ps, '').map((p) => p.id)).toEqual(['1', '2', '3', '4']);
    expect(filterProjects(ps, '   ').map((p) => p.id)).toEqual(['1', '2', '3', '4']);
  });

  it('ranks names that start with it, then names that contain it, then paths', () => {
    expect(filterProjects(ps, 'web').map((p) => p.id)).toEqual(['1', '2', '3']);
    expect(filterProjects(ps, 'gate').map((p) => p.id)).toEqual(['3']);
    expect(filterProjects(ps, 'SITE').map((p) => p.id)).toEqual(['1']);
    expect(filterProjects(ps, 'zzz')).toEqual([]);
  });

  it('does not change the list it is given', () => {
    const copy = [...ps];
    filterProjects(ps, 'doc');
    expect(ps).toEqual(copy);
  });
});

describe('what a project asks of the user', () => {
  const a = (over: Partial<ProjectActivity> = {}): ProjectActivity => ({
    projectId: 'p',
    name: 'P',
    needsInput: 0,
    blocked: 0,
    idle: 0,
    running: 0,
    failed: 0,
    review: 0,
    repoAttention: 0,
    repoRisk: 0,
    ...over,
  });

  it('is said in a few words, and nothing when quiet', () => {
    expect(activitySummary(undefined)).toBe('');
    expect(activitySummary(a())).toBe('');
    expect(activitySummary(a({ needsInput: 1, blocked: 2, idle: 1, running: 3 }))).toBe('1 needs input · 2 blocked · 1 waiting · 3 running');
    expect(activitySummary(a({ needsInput: 2 }))).toBe('2 need input');
  });

  it('puts exceptions before activity, and counts Git findings once', () => {
    expect(activitySummary(a({ failed: 1, review: 2, running: 4 }))).toBe('1 failed · 2 to review · 4 running');
    // A risk is also an attention-level finding: it must not be said twice.
    expect(activitySummary(a({ repoAttention: 3, repoRisk: 1 }))).toBe('1 repository risk · 2 Git items');
    expect(activitySummary(a({ repoAttention: 1 }))).toBe('1 Git item');
    expect(activitySummary(a({ repoAttention: 2, repoRisk: 2 }))).toBe('2 repository risks');
    expect(activitySummary(a({ needsInput: 1, blocked: 1, failed: 1, repoRisk: 1, repoAttention: 1, review: 1, idle: 1, running: 1 }))).toBe(
      '1 needs input · 1 blocked · 1 failed · 1 repository risk · 1 to review · 1 waiting · 1 running',
    );
  });

  it('counts what needs the user, not what is merely running', () => {
    expect(attentionCount(undefined)).toBe(0);
    expect(attentionCount(a({ running: 5 }))).toBe(0);
    expect(attentionCount(a({ needsInput: 1, blocked: 1, idle: 1, running: 5 }))).toBe(3);
  });

  it('lights the badge for failures and repository risk, not for housekeeping or finished work', () => {
    expect(attentionCount(a({ failed: 2, repoRisk: 1 }))).toBe(3);
    expect(attentionCount(a({ review: 4 }))).toBe(0);
    expect(attentionCount(a({ repoAttention: 6 }))).toBe(0);
  });
});

describe('moving through the switcher with the keyboard', () => {
  it('wraps at both ends and copes with an empty list', () => {
    expect(stepIndex(0, 1, 3)).toBe(1);
    expect(stepIndex(2, 1, 3)).toBe(0);
    expect(stepIndex(0, -1, 3)).toBe(2);
    expect(stepIndex(-1, 1, 3)).toBe(0);
    expect(stepIndex(-1, -1, 3)).toBe(2);
    expect(stepIndex(0, 1, 0)).toBe(-1);
  });
});
