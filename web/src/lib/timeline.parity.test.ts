// The Team console has no build step, so its timeline arithmetic (internal/team/console/static/timeline.js) is written
// out separately from the individual app's (timeline.ts). This runs the same cases against both, so they cannot drift.
/* eslint-disable @typescript-eslint/no-explicit-any */
import { describe, expect, it } from 'vitest';
import * as ts from './timeline';
import { inkFor } from './labels';
import type { Label, Task } from './types';

// @ts-expect-error the console's script is plain JavaScript, with no type declarations
import consoleTimeline from '../../../internal/team/console/static/timeline.js';

// The console's functions, which take and return plain values.
const js = consoleTimeline as Record<string, (...args: any[]) => any>;

const task = (id: string, plan?: Task['plan'], extra: Partial<Task> = {}): Task =>
  ({ id, projectId: 'p', title: id, description: '', state: 'backlog', position: 1, execution: {}, version: 1, createdAt: '', updatedAt: '', plan, ...extra }) as Task;

describe('the console’s timeline arithmetic agrees with the app’s', () => {
  const plans: Task['plan'][] = [
    undefined,
    {},
    { start: '2026-10-05', end: '2026-10-09' },
    { start: '2026-10-05' },
    { end: '2026-10-09' },
    { start: '2026-10-12', milestone: true },
    { start: 'soon' },
    { start: '2026-10-09', end: '2026-10-05' },
  ];

  it('reads a plan the same way', () => {
    for (const p of plans) expect(js.spanOf({ plan: p })).toEqual(ts.spanOf({ plan: p }));
  });

  it('counts days and draws the same range and ticks', () => {
    expect(js.daysBetween('2026-12-30', '2027-01-02')).toBe(ts.daysBetween('2026-12-30', '2027-01-02'));
    const spans = [{ start: '2026-10-05', end: '2026-10-09', milestone: false }, { start: '2026-11-20', end: '2026-11-25', milestone: false }];
    expect(js.viewRange(spans, '2026-10-10')).toEqual(ts.viewRange(spans, '2026-10-10'));
    expect(js.viewRange([], '2026-10-10')).toEqual(ts.viewRange([], '2026-10-10'));
    for (const zoom of ['week', 'month', 'quarter'] as const) {
      const range = { start: '2026-10-01', end: '2026-12-31' };
      expect(js.ticks(range, zoom)).toEqual(ts.ticks(range, zoom));
    }
  });

  it('places bars and arrows the same way', () => {
    const range = { start: '2026-10-01', end: '2026-12-31' };
    const tasks = [
      task('a', { start: '2026-10-05', end: '2026-10-09' }),
      task('b', { start: '2026-10-07', end: '2026-10-12' }, { orchestration: { enabled: false, dependencies: ['a'], expectedPaths: [] } }),
      task('c', { start: '2026-10-20', milestone: true }),
    ];
    for (const zoom of ['week', 'month', 'quarter'] as const) {
      const tb = ts.barsFor(tasks, range, zoom);
      const jb = js.barsFor(tasks, range, zoom);
      expect([...jb.entries()]).toEqual([...tb.entries()]);
      const rows = new Map([['a', 0], ['b', 1], ['c', 2]]);
      expect(js.arrows(jb, rows, js.dependencyMap(tasks))).toEqual(ts.arrows(tb, rows, ts.dependencyMap(tasks)));
    }
  });

  it('groups the same way', () => {
    const labels: Label[] = [{ id: 'b', name: 'Beta', color: '#000000', version: 1 }, { id: 'a', name: 'alpha', color: '#111111', version: 1 }];
    const tasks = [
      task('late', { start: '2026-10-20' }, { labelIds: ['a'] }),
      task('early', { start: '2026-10-05', end: '2026-10-07' }, { labelIds: ['a', 'b'] }),
      task('undated', undefined, { labelIds: [], workMode: 'human', state: 'review' }),
    ];
    for (const by of ['none', 'label', 'status', 'mode'] as const) {
      // Each product names its own statuses (the app's "Review" is Team's "In review"), so those titles are not compared.
      const strip = (gs: { key: string; title: string; color?: string; tasks: Task[] }[]) => gs.map((g) => [g.key, by === 'status' ? '' : g.title, g.color, g.tasks.map((t) => t.id)]);
      expect(strip(js.groupTasks(tasks, labels, by))).toEqual(strip(ts.groupTasks(tasks, labels, by)));
    }
  });

  it('words dates and picks ink the same way', () => {
    for (const s of [{ start: '2026-10-05', end: '2026-10-09' }, { start: '2026-10-28', end: '2026-11-02' }, { start: '2027-01-05', end: '2027-01-05' }]) {
      expect(js.spanTitle({ ...s, milestone: false }, 2026)).toBe(ts.spanTitle({ ...s, milestone: false }, 2026));
    }
    for (const c of ['#000000', '#ffffff', '#d0a215', '#4385be', '#1a237e', 'nope']) expect(js.inkFor(c)).toBe(inkFor(c));
    expect(js.localToday(new Date(2026, 9, 5, 23, 59).getTime())).toBe(ts.localToday(new Date(2026, 9, 5, 23, 59).getTime()));
  });

  it('never uses a colour that is not #rrggbb', () => {
    expect(js.safeColor('#33aa77')).toBe('#33aa77');
    for (const bad of ['red', 'url(x)', '#12', 'javascript:1', undefined, '#12345g']) expect(js.safeColor(bad)).toBe('#6f6e69');
  });
});
