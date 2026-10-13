import { expect, it } from 'vitest';
import { analyticsDuration, measuredCost, measuredNumber } from './analytics';
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
