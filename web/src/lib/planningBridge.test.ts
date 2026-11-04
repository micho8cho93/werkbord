import { beforeEach, describe, expect, it, vi } from 'vitest';

const task = { id: 'tsk_1', title: 'Write', description: 'Words', version: 3, plan: { start: '2026-11-05', end: '2026-11-05', time: '09:30' }, orchestration: undefined, archivedAt: undefined };
const listTasks = vi.fn();
const editTask = vi.fn();
vi.mock('./api', () => ({ api: { listTasks: (...a: unknown[]) => listTasks(...a), editTask: (...a: unknown[]) => editTask(...a) } }));
const { planningOperation, PLANNING_HREF } = await import('./planningBridge');

const href = '#/p/prj_a/task/tsk_1';
beforeEach(() => { listTasks.mockReset().mockResolvedValue([task]); editTask.mockReset().mockImplementation(async (t, patch) => ({ ...t, ...patch, version: t.version + 1 })); });

describe('planningOperation: what the Calendar reads and changes of a task', () => {
  it('only aims at a task of a project, by its address', async () => {
    expect(PLANNING_HREF.test(href)).toBe(true);
    for (const bad of ['#/p/prj_a/work', '#/p/prj_a/task/', 'https://example.com/#/p/a/task/b', '#/p/a/task/b/../c']) expect(PLANNING_HREF.test(bad)).toBe(false);
    await expect(planningOperation('#/p/prj_a/work', 'read')).rejects.toThrow('This task address is invalid.');
    expect(listTasks).not.toHaveBeenCalled();
  });

  it('reads a task without changing it', async () => {
    const item = await planningOperation(href, 'read');
    expect(item).toMatchObject({ title: 'Write', version: 3, start: '2026-11-05', end: '2026-11-05', time: '09:30', editable: true });
    expect(editTask).not.toHaveBeenCalled();
  });

  it('refuses a change made against an older version, and a task that is gone', async () => {
    await expect(planningOperation(href, 'move', { version: 2, start: '2026-11-06', end: '2026-11-06' })).rejects.toThrow('changed elsewhere');
    expect(editTask).not.toHaveBeenCalled();
    listTasks.mockResolvedValue([]);
    await expect(planningOperation(href, 'read')).rejects.toThrow('no longer available');
  });

  it('moves a task to a day, clearing its time of day when it now spans days, and keeping it for a single day', async () => {
    await planningOperation(href, 'move', { version: 3, start: '2026-11-06', end: '2026-11-08' });
    expect(editTask.mock.calls[0][1].plan).toMatchObject({ start: '2026-11-06', end: '2026-11-08', time: undefined });
    await planningOperation(href, 'move', { version: 3, start: '2026-11-06', end: '2026-11-06' });
    expect(editTask.mock.calls[1][1].plan).toMatchObject({ start: '2026-11-06', end: '2026-11-06', time: '09:30' });
  });

  it('saves a task’s words with its dates', async () => {
    await planningOperation(href, 'save', { version: 3, title: 'Write notes', description: 'More', start: '2026-11-05', end: '2026-11-05', time: '' });
    expect(editTask.mock.calls[0][1]).toMatchObject({ title: 'Write notes', description: 'More' });
  });
});
