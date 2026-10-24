import { expect, it } from 'vitest';
import { analyticsDuration, executionTotals, type AnalyticsExecution, measuredCost, measuredNumber } from './analytics';
import { inMore, moreEntries, parse, tabsFor } from './location';

it('preserves unknown measurements separately from reported zero', () => {
  expect(measuredNumber(null)).toBe('Not reported');
  expect(measuredNumber(undefined)).toBe('Not reported');
  expect(measuredNumber(0)).toBe('0');
  expect(measuredCost(null)).toBe('Not reported');
  expect(measuredCost(0)).not.toBe('Not reported');
  expect(analyticsDuration(null)).toBe('No samples');
  expect(analyticsDuration(0)).toBe('0s');
});
it('reaches workspace analytics from More and preserves old project links', () => {
  expect(parse('#/p/one/analytics')).toEqual({ view: 'analytics', projectId: 'one', taskId: '' });
  expect(tabsFor('work').some(t => t.id === 'analytics')).toBe(false);
  expect(moreEntries('one').find(t => t.id === 'analytics')?.href).toBe('#/analytics');
  expect(inMore('analytics')).toBe(true);
});


it('aggregates runner evidence without treating absent usage as measured zero', () => {
  const row: AnalyticsExecution = { projectId: 'p', runnerId: 'a', provider: 'codex', model: '', attempts: 2, retries: 1, outcomes: { completed: 1, failed: 1 }, accepted: 1, rejected: 0, limitEvents: 0, inputTokens: 120, outputTokens: 0, cachedTokens: null, inputRuns: 1, outputRuns: 1, cachedRuns: 0, partialRuns: 1, actualCostUsd: null, estimatedCostUsd: null, actualCostRuns: 0, estimatedCostRuns: 0 };
  const totals = executionTotals([row, { ...row, runnerId: 'b', inputTokens: null, inputRuns: 0 }]);
  expect(totals.inputTokens).toBe(120);
  expect(totals.outputTokens).toBe(0);
  expect(totals.cachedTokens).toBeNull();
  expect(totals.actualCostUsd).toBeNull();
  expect(totals.inputRuns).toBe(1);
  expect(totals.attempts).toBe(4);
  expect(totals.outcomes.completed).toBe(2);
  expect(executionTotals([]).inputTokens).toBeNull();
});

import { cumulative, niceAxis, percent, smoothPath, sumBy } from './analytics';
it('draws running totals, round axes and honest percentages', () => {
  expect(cumulative([{ date: 'a', created: 2, completed: 0 }, { date: 'b', created: 1, completed: 2 }]).map((d) => [d.created, d.completed])).toEqual([[2, 0], [3, 2]]);
  expect(percent(1, 0)).toBeNull();
  expect(percent(1, 3)).toBe(33);
  expect(niceAxis(0).top).toBeGreaterThanOrEqual(1);
  expect(niceAxis(1).ticks).toEqual([0, 1]);
  expect(niceAxis(9).ticks.every(Number.isInteger)).toBe(true);
  expect(percent(3, 2)).toBe(150);
  const axis = niceAxis(37);
  expect(axis.top).toBeGreaterThanOrEqual(37);
  expect(axis.ticks[0]).toBe(0);
  expect(axis.ticks.length).toBeLessThanOrEqual(6);
});
it('sums by key without turning unknown into zero, and never overshoots a line', () => {
  const row = (provider: string, v: number | null) => ({ provider, runnerId: '', projectId: '', model: '', attempts: 1, retries: 0, outcomes: {}, accepted: 0, rejected: 0, limitEvents: 0, inputTokens: v, outputTokens: null, cachedTokens: null, inputRuns: 0, outputRuns: 0, cachedRuns: 0, partialRuns: 0, actualCostUsd: null, estimatedCostUsd: null, actualCostRuns: 0, estimatedCostRuns: 0 });
  const sums = sumBy([row('a', 5), row('a', 2), row('b', null)], 'provider', (r) => r.inputTokens);
  expect(sums).toEqual([{ key: 'a', value: 7 }, { key: 'b', value: null }]);
  const d = smoothPath([[0, 10], [10, 10], [20, 0], [30, 0]]);
  expect(d.startsWith('M0,10 C')).toBe(true);
  const ys = [...d.matchAll(/,(-?[\d.]+)/g)].map((m) => +m[1]);
  expect(Math.min(...ys)).toBeGreaterThanOrEqual(0);
  expect(Math.max(...ys)).toBeLessThanOrEqual(10);
});
