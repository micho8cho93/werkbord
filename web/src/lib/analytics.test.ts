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
