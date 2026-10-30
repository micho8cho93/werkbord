import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import AnalyticsUsageDetails from './AnalyticsUsageDetails.svelte';
import type { AnalyticsExecution } from './analytics';

const row: AnalyticsExecution = {
  projectId: 'p', runnerId: 'local', provider: 'codex', model: 'configured', attempts: 1, retries: 0,
  outcomes: {}, accepted: 0, rejected: 0, limitEvents: 0, inputTokens: 100, outputTokens: null,
  cachedTokens: null, inputRuns: 1, outputRuns: 0, cachedRuns: 0, partialRuns: 1,
  actualCostUsd: null, estimatedCostUsd: 2, actualCostRuns: 0, estimatedCostRuns: 1,
};
describe('retained usage reporting', () => {
  it('combines matching provider/models across projects without converting unknown costs to zero', () => {
    const html = render(AnalyticsUsageDetails, {props: {
      rows: [row, {...row, projectId: 'another', runnerId: 'remote'}], scope: 'all',
      days: [{date: '2026-10-01', created: 0, completed: 0, attempts: 2, inputTokens: 0, inputRuns: 1}],
    }}).body;
    expect(html.match(/codex · configured/g)).toHaveLength(1);
    expect(html).toContain('200 / Not reported');
    expect(html).toContain('$4.00');
    expect(html).toContain('Not reported (0 reports)');
    expect(html).toContain('2 attempts · 1 input reports');
    expect(html).toContain('not daily spend');
  });
  it('reuses trends without duplicating the browser’s existing model breakdown', () => {
    const html = render(AnalyticsUsageDetails, {props: {rows: [], days: [], scope: 'p', showModels: false}}).body;
    expect(html).not.toContain('<summary>Provider and model usage');
    expect(html).toContain('Usage history is unavailable');
  });
});
