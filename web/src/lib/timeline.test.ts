import { describe, expect, it } from 'vitest';
import { arrows, barFor, barsFor, dependencyMap, daysBetween, firstRows, groupTasks, localToday, PX_PER_DAY, ROW_HEIGHT, spanOf, spanTitle, ticks, viewRange, warningsByTask, worst } from './timeline';
import type { Label, Task, TimelineWarning } from './types';

const task = (id: string, plan?: Task['plan'], extra: Partial<Task> = {}): Task =>
  ({ id, projectId: 'p', title: id, description: '', state: 'backlog', position: 1, execution: {}, version: 1, createdAt: '', updatedAt: '', plan, ...extra }) as Task;

describe('spans', () => {
  it('reads a plan as inclusive whole days', () => {
    expect(spanOf(task('a', { start: '2026-10-05', end: '2026-10-09' }))).toEqual({ start: '2026-10-05', end: '2026-10-09', milestone: false });
    expect(spanOf(task('a', { start: '2026-10-05' }))).toEqual({ start: '2026-10-05', end: '2026-10-05', milestone: false });
    expect(spanOf(task('a', { end: '2026-10-09' }))).toEqual({ start: '2026-10-09', end: '2026-10-09', milestone: false });
    expect(spanOf(task('a', { start: '2026-10-12', milestone: true }))).toEqual({ start: '2026-10-12', end: '2026-10-12', milestone: true });
  });

  it('places nothing it cannot read', () => {
    expect(spanOf(task('a'))).toBeNull();
    expect(spanOf(task('a', {}))).toBeNull();
    expect(spanOf(task('a', { start: 'soon' }))).toBeNull();
    expect(spanOf(task('a', { start: '2026-10-09', end: '2026-10-05' }))).toBeNull();
  });

  it('counts days across month and year ends', () => {
    expect(daysBetween('2026-10-05', '2026-10-09')).toBe(4);
    expect(daysBetween('2026-12-30', '2027-01-02')).toBe(3);
    expect(daysBetween('2026-10-09', '2026-10-05')).toBe(-4);
  });

  it('titles spans briefly', () => {
    expect(spanTitle({ start: '2026-10-05', end: '2026-10-09', milestone: false }, 2026)).toBe('Oct 5 – 9');
    expect(spanTitle({ start: '2026-10-28', end: '2026-11-02', milestone: false }, 2026)).toBe('Oct 28 – Nov 2');
    expect(spanTitle({ start: '2026-10-05', end: '2026-10-05', milestone: true }, 2026)).toBe('Oct 5');
    expect(spanTitle({ start: '2027-01-05', end: '2027-01-05', milestone: false }, 2026)).toBe('Jan 5, 2027');
  });
});

describe('the drawn range', () => {
  it('covers every span and today, with air', () => {
    const r = viewRange([{ start: '2026-10-05', end: '2026-10-09', milestone: false }, { start: '2026-11-20', end: '2026-11-25', milestone: false }], '2026-10-10', 7, 10);
    expect(r).toEqual({ start: '2026-09-28', end: '2026-12-02' });
  });

  it('is never shorter than the minimum', () => {
    const r = viewRange([], '2026-10-10', 1, 28);
    expect(daysBetween(r.start, r.end) + 1).toBe(28);
    expect(daysBetween(r.start, '2026-10-10')).toBe(1);
  });
});

describe('bars and ticks', () => {
  const range = { start: '2026-10-01', end: '2026-12-31' };
  it('places a bar from the left of its first day to the right of its last', () => {
    const b = barFor('a', { start: '2026-10-05', end: '2026-10-09', milestone: false }, range, 'week');
    expect(b.x).toBe(4 * PX_PER_DAY.week);
    expect(b.width).toBe(5 * PX_PER_DAY.week);
    const m = barFor('m', { start: '2026-10-05', end: '2026-10-05', milestone: true }, range, 'month');
    expect(m.width).toBe(PX_PER_DAY.month);
  });

  it('ticks days, Mondays or months by zoom', () => {
    const week = ticks({ start: '2026-10-01', end: '2026-10-14' }, 'week');
    expect(week).toHaveLength(14);
    expect(week[0].label).toBe('Oct 1');
    const month = ticks({ start: '2026-10-01', end: '2026-10-31' }, 'month');
    expect(month.map((t) => t.day)).toEqual(['2026-10-05', '2026-10-12', '2026-10-19', '2026-10-26']);
    const quarter = ticks(range, 'quarter');
    expect(quarter.map((t) => t.label)).toEqual(['Oct', 'Nov', 'Dec']);
  });
});

describe('grouping', () => {
  const labels: Label[] = [{ id: 'b', name: 'Beta', color: '#000000', version: 1 }, { id: 'a', name: 'alpha', color: '#111111', version: 1 }];
  const tasks = [
    task('late', { start: '2026-10-20' }, { labelIds: ['a'] }),
    task('early', { start: '2026-10-05', end: '2026-10-07' }, { labelIds: ['a', 'b'] }),
    task('undated', undefined, { labelIds: [], workMode: 'human', state: 'doing' }),
  ];

  it('puts earlier work first and undated work last', () => {
    const [g] = groupTasks(tasks, labels, 'none');
    expect(g.tasks.map((t) => t.id)).toEqual(['early', 'late', 'undated']);
  });

  it('lists a task under each of its labels, and unlabelled work under none', () => {
    const gs = groupTasks(tasks, labels, 'label');
    expect(gs.map((g) => [g.title, g.tasks.map((t) => t.id)])).toEqual([
      ['alpha', ['early', 'late']],
      ['Beta', ['early']],
      ['No label', ['undated']],
    ]);
    expect(gs[0].color).toBe('#111111');
  });

  it('groups by status in board order and by mode', () => {
    expect(groupTasks(tasks, labels, 'status').map((g) => g.title)).toEqual(['Backlog', 'Doing']);
    expect(groupTasks(tasks, labels, 'mode').map((g) => [g.title, g.tasks.length])).toEqual([['Human', 1], ['Agent', 2]]);
  });
});

describe('dependency arrows', () => {
  const range = { start: '2026-10-01', end: '2026-12-31' };
  const bar = (id: string, s: string, e: string) => [id, barFor(id, { start: s, end: e, milestone: false }, range, 'week')] as const;

  it('draws from the end of the first bar to the start of the second', () => {
    const bars = new Map([bar('a', '2026-10-05', '2026-10-06'), bar('b', '2026-10-09', '2026-10-10')]);
    const rows = new Map([['a', 0], ['b', 1]]);
    const [ar] = arrows(bars, rows, new Map([['b', ['a']]]));
    expect(ar.from).toBe('a');
    expect(ar.to).toBe('b');
    expect(ar.conflict).toBe(false);
    const x1 = bars.get('a')!.x + bars.get('a')!.width;
    expect(ar.d.startsWith(`M${x1} ${ROW_HEIGHT / 2}`)).toBe(true);
    expect(ar.d).toContain(`V${ROW_HEIGHT + ROW_HEIGHT / 2}`);
    expect(ar.d.endsWith(`H${bars.get('b')!.x}`)).toBe(true);
  });

  it('marks an arrow whose dependent starts before its dependency ends', () => {
    const bars = new Map([bar('a', '2026-10-05', '2026-10-09'), bar('b', '2026-10-07', '2026-10-12')]);
    const [ar] = arrows(bars, new Map([['a', 0], ['b', 1]]), new Map([['b', ['a']]]));
    expect(ar.conflict).toBe(true);
    // Starting the day a dependency ends is allowed.
    const same = new Map([bar('a', '2026-10-05', '2026-10-09'), bar('b', '2026-10-09', '2026-10-12')]);
    expect(arrows(same, new Map([['a', 0], ['b', 1]]), new Map([['b', ['a']]]))[0].conflict).toBe(false);
  });

  it('skips dependencies with an end that is not on the chart', () => {
    const bars = new Map([bar('b', '2026-10-07', '2026-10-12')]);
    expect(arrows(bars, new Map([['b', 0]]), new Map([['b', ['a']]]))).toEqual([]);
  });
});

describe('warnings', () => {
  const w = (code: string, severity: TimelineWarning['severity'], itemId: string, extra: Partial<TimelineWarning> = {}): TimelineWarning => ({ code, severity, itemId, message: code, ...extra });

  it('files a warning under its task and under every task in a cycle', () => {
    const by = warningsByTask([w('x', 'warning', 'a'), w('cycle', 'error', 'a', { cycle: ['a', 'b'] })]);
    expect(by.get('a')).toHaveLength(2);
    expect(by.get('b')).toHaveLength(1);
    expect(worst(by.get('a'))).toBe('error');
    expect(worst(by.get('b'))).toBe('error');
    expect(worst([w('i', 'info', 'z')])).toBe('info');
    expect(worst(undefined)).toBe('');
  });
});

describe('today', () => {
  it('is the date on this computer’s calendar', () => {
    expect(localToday(new Date(2026, 9, 5, 23, 59).getTime())).toBe('2026-10-05');
    expect(localToday(new Date(2026, 0, 1, 0, 0).getTime())).toBe('2026-01-01');
  });
});

describe('helpers that build the chart', () => {
  const range = { start: '2026-10-01', end: '2026-12-31' };
  it('draws bars only for tasks with dates', () => {
    const bars = barsFor([task('a', { start: '2026-10-05' }), task('b'), task('c', { start: '2026-10-07', milestone: true })], range, 'week');
    expect([...bars.keys()]).toEqual(['a', 'c']);
    expect(bars.get('c')?.milestone).toBe(true);
  });

  it('remembers the first row of a task listed more than once, and skips headers', () => {
    const rows = firstRows([null, 'a', 'b', null, 'a']);
    expect(rows.get('a')).toBe(1);
    expect(rows.get('b')).toBe(2);
    expect(rows.size).toBe(2);
  });

  it('lists only the tasks that wait for something', () => {
    const m = dependencyMap([task('a'), { ...task('b'), orchestration: { enabled: false, dependencies: ['a'], expectedPaths: [] } }]);
    expect([...m.entries()]).toEqual([['b', ['a']]]);
  });
});
