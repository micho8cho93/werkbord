import { describe, expect, it } from 'vitest';
import { needAction, readyCount, rowsOf, stateOf, urgentFirst } from './workqueue';
import type { Attention, Entry, Overview, QueueItem, Summary } from './types';

const ws: Entry = { id: 'personal', kind: 'personal', name: 'Personal', state: 'ready' };
const item = (id: string, group: QueueItem['group'], extra: Partial<QueueItem> = {}): QueueItem => ({ id, title: id, project: 'App', href: `#/p/a/task/${id}`, group, ...extra });
const att = (id: string, kind: string, href: string, severity: Attention['severity'] = 'warning'): Attention => ({ id, kind, severity, title: id, href });
const overview = (attention: Attention[], work: Summary['work'] = []): Overview => ({ at: '', entries: [{ workspace: ws, summary: { schema: 'workspace-v1', workspace: ws, at: '', projects: [], work, attention, schedule: [] } }] });

describe('what state a task is in', () => {
  it('tells action, blocked, running, waiting, ready and done apart', () => {
    expect(stateOf(item('a', 'now'), undefined, [att('x', 'review', '#')])).toBe('action');
    expect(stateOf(item('a', 'now'), undefined, [att('x', 'blocked', '#')])).toBe('blocked');
    expect(stateOf(item('a', 'now', { detail: 'Blocked by another task' }), undefined, [])).toBe('blocked');
    expect(stateOf(item('a', 'now'), 'running', [])).toBe('running');
    expect(stateOf(item('a', 'next'), 'queued', [])).toBe('waiting');
    expect(stateOf(item('a', 'next'), undefined, [])).toBe('ready');
    expect(stateOf(item('a', 'done'), 'running', [att('x', 'failed', '#')])).toBe('done');
  });
  it('a question makes a running task one that needs action', () => {
    expect(stateOf(item('a', 'now'), 'running', [att('q', 'needs_input', '#')])).toBe('action');
  });
  it('ignores host and runner advice, which is about machines and not about this task', () => {
    const o = overview([att('h', 'host', '#/p/a/task/a', 'warning')]);
    expect(rowsOf(o, [{ workspace: 'personal', item: item('a', 'now') }])[0].state).toBe('ready');
  });
});

describe('the counts beside Now and Next', () => {
  const o = overview([att('q', 'needs_input', '#/p/a/task/1'), att('r', 'review', '#/p/a/task/2'), att('b', 'blocked', '#/p/a/task/3'), att('q2', 'needs_input', '#/p/a/task/1')],
    [{ id: '4', title: '4', project: 'App', status: 'doing', execution: 'running', href: '#/p/a/task/4' }, { id: '5', title: '5', project: 'App', status: 'todo', execution: 'queued', href: '#/p/a/task/5' }]);
  const now = rowsOf(o, ['1', '2', '3', '4'].map((id) => ({ workspace: 'personal', item: item(id, 'now') })));
  const next = rowsOf(o, ['5', '6', '7'].map((id) => ({ workspace: 'personal', item: item(id, 'next') })));
  it('counts tasks that need action once each, not their questions and not blocked or running ones', () => {
    expect(needAction(now)).toBe(2);
    expect(now.filter((r) => r.asking).map((r) => r.item.id)).toEqual(['1']);
  });
  it('counts only what can be started as it is as ready', () => expect(readyCount(next)).toBe(2));
  it('puts what needs the person first and keeps the given order otherwise', () => {
    expect(urgentFirst(now).map((r) => r.item.id)).toEqual(['1', '2', '3', '4']);
    expect(urgentFirst([...now].reverse()).map((r) => r.item.id)).toEqual(['2', '1', '3', '4']);
  });
  it('does not take a task for running when its report is stale', () => {
    const stale = overview([], [{ id: '4', title: '4', project: 'App', status: 'doing', execution: 'running', href: '#/p/a/task/4', report: { stale: true } }]);
    expect(rowsOf(stale, [{ workspace: 'personal', item: item('4', 'now') }])[0].state).toBe('ready');
  });
});
