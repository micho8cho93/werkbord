import type { Component } from 'svelte';
import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import BarChartBlock from './BarChartBlock.svelte';
import ComparisonChartBlock from './ComparisonChartBlock.svelte';
import DataTableBlock from './DataTableBlock.svelte';
import LineChartBlock from './LineChartBlock.svelte';
import PieChartBlock from './PieChartBlock.svelte';
import ProgressBlock from './ProgressBlock.svelte';
import StatusSummaryBlock from './StatusSummaryBlock.svelte';
import TimelineBlock from './TimelineBlock.svelte';
import WorkflowBlock from './WorkflowBlock.svelte';
import DataTableSource from './DataTableBlock.svelte?raw';
import PieChartSource from './PieChartBlock.svelte?raw';
import StatusSummarySource from './StatusSummaryBlock.svelte?raw';
import TimelineSource from './TimelineBlock.svelte?raw';

const asOf = '2026-03-09T09:00:00Z';
function html(component: unknown, props: Record<string, unknown>): string {
  return render(component as Component<Record<string, unknown>>, { props: { props } }).body;
}

describe('Werkbord visualization components', () => {
  it('shows deterministic current progress counts and the observed snapshot', () => {
    const status = html(StatusSummaryBlock, { backlog: 2, doing: 1, review: 1, done: 4, asOf });
    expect(status).toContain('Backlog');
    expect(status).toContain('>4</dd>');
    expect(status).toContain(`Snapshot`);
    expect(status).toContain(asOf);

    const progress = html(ProgressBlock, { label: 'Done', value: 4, total: 8, asOf });
    expect(progress).toContain('4 of 8');
    expect(progress).toContain('max="8"');
    expect(progress).toContain('value="4"');

    const unknownTotal = html(ProgressBlock, { label: 'Done', value: 0, total: 0 });
    expect(unknownTotal).toContain('no total recorded');
    expect(unknownTotal).toContain('No total is recorded');
    expect(unknownTotal).not.toContain('%');
  });

  it('renders charts from recorded application values and explains empty data', () => {
    const bars = html(BarChartBlock, { title: 'Tasks by state', categories: ['Backlog', 'Doing'], values: [2, 1], asOf });
    expect(bars).toContain('Backlog: 2');
    expect(bars).toContain('Doing: 1');
    expect(bars).toContain('width:100%');
    expect(html(BarChartBlock, { title: 'Empty', categories: [], values: [], asOf })).toContain('No chart values');
    expect(bars).toContain('aria-pressed="false"');

    const line = html(LineChartBlock, { title: 'Completed tasks', labels: ['2026-03-08', '2026-03-09'], values: [1, 2], asOf });
    expect(line).toContain('2026-03-08: 1');
    expect(line).toContain('2026-03-09: 2');
    expect(line).toContain('<polyline');
    expect(html(LineChartBlock, { title: 'No history', labels: [], values: [], asOf })).toContain('No dated values');

    const pie = html(PieChartBlock, { title: 'Run outcomes', categories: ['completed', 'failed'], values: [3, 1], asOf });
    expect(pie).toContain('completed');
    expect(pie).toContain('75.0%');
    expect(pie).toContain('failed');
    expect(html(PieChartBlock, { title: 'No runs', categories: ['completed'], values: [0], asOf })).toContain('No non-zero values');

    const comparison = html(ComparisonChartBlock, { title: 'Provider runs', categories: ['Attempts'], firstLabel: 'Claude', firstValues: [5], secondLabel: 'Codex', secondValues: [3], asOf });
    expect(comparison).toContain('Claude');
    expect(comparison).toContain('Codex');
    expect(comparison).toContain('width:100%');
    expect(comparison).toContain('Compare category');
    expect(comparison).toContain('Totals:');
    expect(html(ComparisonChartBlock, { title: 'No comparison', categories: [], firstValues: [], secondValues: [], asOf })).toContain('No paired values');
  });

  it('keeps tables interactive and timelines honest about missing planned dates', () => {
    const table = html(DataTableBlock, { caption: 'Agent runs', columns: ['Provider', 'State'], rows: [['Claude', 'blocked'], ['Codex', 'ready']], filters: [{ column: 'State', label: 'Filter by state', options: ['blocked', 'ready', 'missing'] }], asOf });
    expect(table).toContain('Filter rows');
    expect(table).toContain('aria-label="Filter by state"');
    expect(table).not.toContain('missing</option>');
    expect(table).toContain('Sort by Provider');
    expect(table).toContain('Claude');
    expect(table).toContain('Codex');
    expect(html(DataTableBlock, { caption: 'No rows', columns: ['Task'], rows: [], asOf })).toContain('No records are available');

    const timeline = html(TimelineBlock, { title: 'Release plan', items: [
      { id: 'task-1', title: 'Implement filters', start: '2026-03-10', end: '2026-03-12', state: 'doing', dependsOn: ['task-0'] },
      { id: 'task-2', title: 'Review API', start: '', end: '', state: 'backlog' },
    ], asOf });
    expect(timeline).toContain('Implement filters');
    expect(timeline).toContain('2026-03-10');
    expect(timeline).toContain('Dates incomplete');
    expect(timeline).toContain('No planned range recorded');
    expect(html(TimelineBlock, { title: 'Empty schedule', items: [], asOf })).toContain('No scheduled tasks');
    const dated = html(LineChartBlock, { title: 'Dated values', labels: ['2026-03-08', '2026-03-09'], values: [1, 2], asOf });
    expect(dated).toContain('Filter from date');
    expect(dated).toContain('Filter to date');
  });

  it('shows task dependencies and blockers without joining unknown IDs to invented records', () => {
    const workflow = html(WorkflowBlock, { title: 'Execution', items: [
      { id: 'task-1', title: 'Implement filters', state: 'doing', runState: 'running', dependsOn: ['task-0', 'missing'], blocker: 'Waiting for API access' },
      { id: 'task-0', title: 'Define API', state: 'done' },
    ], asOf });
    expect(workflow).toContain('Latest run: running');
    expect(workflow).toContain('Depends on Define API, Unlisted task missing');
    expect(workflow).toContain('Waiting for API access');
    expect(html(WorkflowBlock, { title: 'Empty workflow', items: [], asOf })).toContain('No task relationships');
  });

  it('keeps the compact tables and diagrams responsive at phone widths', () => {
    for (const source of [DataTableSource, StatusSummarySource, TimelineSource, PieChartSource]) {
      expect(source).toMatch(/@media \(max-width:/);
      expect(source).toContain('min-width: 0');
    }
    expect(DataTableSource).toContain('overflow: auto');
    expect(TimelineSource).toContain('grid-template-columns: minmax(0, 1fr)');
    expect(DataTableSource).toContain('min-height: 44px');
  });
});
