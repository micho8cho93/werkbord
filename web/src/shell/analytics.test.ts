import { describe, expect, it } from 'vitest';
import { activityOf, mergeActivity, projectsWithin, reads, runsAvailable, runsOf, wheres } from './analytics';
import type { Item, Overview } from './types';

const item = (id: string, kind: Item['kind'], name: string, state: Item['state'] = 'ready'): Item => ({ id, kind, name, state, source: 'x' });
const items = [item('personal', 'personal', 'Personal'), item('team:a', 'team', 'Acme'), item('team:b', 'team', 'Beta'), item('team:x', 'team', 'Down', 'offline')];

const personalReport = {
  created: 10, completed: 6, durationSamples: 4, averageTaskSeconds: 100, medianTaskSeconds: 80,
  days: [{ date: '2026-10-01', created: 2, completed: 1 }, { date: '2026-10-02', created: 8, completed: 5 }],
  categories: [{ labelId: 'l1', name: 'Bug', created: 4, completed: 3 }],
  truncated: false, retentionRemoved: false, missingTaskHistory: 0, end: '2026-10-25T00:00:00Z',
};
const teamReport = {
  created: 5, completed: 5, durationSamples: 1, averageTicketSeconds: 400, medianTicketSeconds: 400,
  days: [{ date: '2026-10-02', created: 1, completed: 2 }, { date: '2026-10-03', created: 4, completed: 3 }],
  categories: [{ labelId: 'l9', name: 'bug', created: 1, completed: 1 }, { labelId: 'l8', name: 'Docs', created: 2, completed: 2 }],
  truncated: true, end: '2026-10-26T00:00:00Z',
};

describe('what each kind of workspace reports about the work itself', () => {
  it('reads Personal’s and a team’s differently named fields into one shape', () => {
    expect(activityOf(personalReport)).toMatchObject({ created: 10, completed: 6, durationSamples: 4, averageSeconds: 100, medianSeconds: 80, partial: false });
    expect(activityOf(teamReport)).toMatchObject({ created: 5, completed: 5, averageSeconds: 400, medianSeconds: 400, partial: true });
  });

  it('does not invent what is not there: nothing reported is zero or unknown, and garbage is ignored', () => {
    expect(activityOf(null)).toMatchObject({ created: 0, completed: 0, averageSeconds: null, medianSeconds: null, days: [], categories: [], partial: false });
    expect(activityOf({ created: 'many', days: [{ date: '' }, { date: '2026-10-01', created: 1 }], categories: [{ name: '' }] }).days).toEqual([{ date: '2026-10-01', created: 1, completed: 0 }]);
  });
});

describe('merging the parts the person selected', () => {
  const merged = mergeActivity([activityOf(personalReport), activityOf(teamReport)]);

  it('adds counts, days by date and categories by name, whatever their case', () => {
    expect(merged.created).toBe(15);
    expect(merged.completed).toBe(11);
    expect(merged.days).toEqual([{ date: '2026-10-01', created: 2, completed: 1 }, { date: '2026-10-02', created: 9, completed: 7 }, { date: '2026-10-03', created: 4, completed: 3 }]);
    expect(merged.categories.map((c) => [c.name, c.created, c.completed])).toEqual([['Bug', 5, 4], ['Docs', 2, 2]]);
  });

  it('weights the average by its samples and gives no median, which cannot be merged', () => {
    expect(merged.averageSeconds).toBeCloseTo((100 * 4 + 400 * 1) / 5);
    expect(merged.medianSeconds).toBeNull();
    expect(merged.durationSamples).toBe(5);
  });

  it('says it is partial if any part is, and a single part is returned as it is, median and all', () => {
    expect(merged.partial).toBe(true);
    expect(merged.end).toBe('2026-10-26T00:00:00Z');
    const one = activityOf(personalReport);
    expect(mergeActivity([one])).toBe(one);
  });
});

describe('what only Personal reports', () => {
  it('totals Personal’s runs and keeps unknown usage unknown', () => {
    const r = runsOf({ executions: [
      { provider: 'claude', attempts: 3, retries: 1, outcomes: { completed: 2, failed: 1 }, inputTokens: 100, outputTokens: null, actualCostUsd: null, accepted: 1, rejected: 0, limitEvents: 0, inputRuns: 1, outputRuns: 0, cachedRuns: 0, actualCostRuns: 0, estimatedCostRuns: 0, partialRuns: 0, cachedTokens: null, estimatedCostUsd: null },
      { provider: 'codex', attempts: 5, retries: 0, outcomes: { completed: 5 }, inputTokens: 50, outputTokens: null, actualCostUsd: null, accepted: 0, rejected: 0, limitEvents: 0, inputRuns: 1, outputRuns: 0, cachedRuns: 0, actualCostRuns: 0, estimatedCostRuns: 0, partialRuns: 0, cachedTokens: null, estimatedCostUsd: null },
    ], accepted: 2, verifiedMerges: 1 });
    expect(r.totals).toMatchObject({ attempts: 8, retries: 1, inputTokens: 150, outputTokens: null, actualCostUsd: null });
    expect(r.providers).toEqual([{ name: 'codex', attempts: 5 }, { name: 'claude', attempts: 3 }]);
    expect(r.accepted).toBe(2);
    expect(runsOf(undefined).totals.attempts).toBe(0);
  });

  it('is available only when Personal is among what is read', () => {
    expect(runsAvailable([{ workspace: 'team:a' }])).toBe(false);
    expect(runsAvailable([{ workspace: 'team:a' }, { workspace: 'personal' }])).toBe(true);
  });
});

describe('the filters', () => {
  const summary = (id: string, kind: 'personal' | 'team', projects: { id: string; name: string; href: string }[]) => ({ workspace: { id, kind, name: id, state: 'ready' as const }, summary: { schema: 's', workspace: { id, kind, name: id, state: 'ready' as const }, projects, work: [], attention: [], schedule: [], at: '' } });
  const overview: Overview = { at: '', entries: [
    summary('personal', 'personal', [{ id: 'p1', name: 'side-project', href: '#' }]),
    summary('team:a', 'team', [{ id: 'w', name: 'web-app', href: '?' }, { id: 'api', name: 'api', href: '?' }]),
    summary('team:x', 'team', [{ id: 'hidden', name: 'unreachable', href: '?' }]),
  ] };

  it('offers everything, Personal only, and each team that can answer, but not one that is down', () => {
    expect(wheres(items).map((w) => [w.id, w.label])).toEqual([['all', 'Personal and all teams'], ['personal', 'Personal only'], ['team:a', 'Acme'], ['team:b', 'Beta']]);
  });

  it('lists the projects of the place chosen, and of every place that can answer when it is everything', () => {
    expect(projectsWithin('all', items, overview).map((p) => p.name)).toEqual(['side-project', 'web-app', 'api']);
    expect(projectsWithin('team:a', items, overview).map((p) => p.name)).toEqual(['web-app', 'api']);
    expect(projectsWithin('personal', items, overview).map((p) => p.name)).toEqual(['side-project']);
    expect(projectsWithin('all', items, null)).toEqual([]);
  });

  it('reads every workspace for a place, and exactly one for a project', () => {
    expect(reads('all', undefined, items)).toEqual([{ workspace: 'personal', project: '' }, { workspace: 'team:a', project: '' }, { workspace: 'team:b', project: '' }]);
    expect(reads('team:b', undefined, items)).toEqual([{ workspace: 'team:b', project: '' }]);
    expect(reads('all', { workspace: 'team:a', id: 'w', name: 'web-app' }, items)).toEqual([{ workspace: 'team:a', project: 'w' }]);
  });
});
