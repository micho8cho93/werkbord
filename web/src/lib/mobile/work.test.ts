import { describe, expect, it } from 'vitest';
import { upcomingNote, workGroups } from './work';
import type { Task } from '../types';

const NOW = new Date(2026, 9, 15, 12).getTime();
const task = (over: Partial<Task>): Task =>
  ({ id: 't', projectId: 'p', title: 'T', description: '', state: 'backlog', position: 0, execution: {}, version: 1, createdAt: '', updatedAt: '2026-10-01T00:00:00Z', ...over }) as Task;

describe('workGroups', () => {
  it('puts a person’s own work, shared work and nothing an agent does alone in "yours"', () => {
    const g = workGroups([task({ id: 'h', workMode: 'human' }), task({ id: 'x', workMode: 'hybrid' }), task({ id: 'a', workMode: 'agent' }), task({ id: 'd', workMode: 'human', state: 'done' })], NOW);
    expect(g.yours.map((t) => t.id).sort()).toEqual(['h', 'x']);
  });

  it('puts work in progress first in "yours"', () => {
    const g = workGroups([task({ id: 'b', workMode: 'human' }), task({ id: 'd', workMode: 'human', state: 'doing' })], NOW);
    expect(g.yours.map((t) => t.id)).toEqual(['d', 'b']);
  });

  it('lists tasks in progress, newest change first', () => {
    const g = workGroups(
      [task({ id: 'old', state: 'doing', updatedAt: '2026-10-01T00:00:00Z' }), task({ id: 'new', state: 'doing', updatedAt: '2026-10-10T00:00:00Z' }), task({ id: 'wait', state: 'backlog' }), task({ id: 'r', state: 'review' })],
      NOW,
    );
    expect(g.active.map((t) => t.id)).toEqual(['new', 'old']);
  });

  it('lists what is under way or coming in the next fortnight, once per task, soonest first', () => {
    const g = workGroups(
      [
        task({ id: 'past', plan: { start: '2026-09-01', end: '2026-09-10' } }),
        task({ id: 'now', plan: { start: '2026-10-14', end: '2026-10-20' } }),
        task({ id: 'soon', plan: { start: '2026-10-18', end: '2026-10-19' } }),
        task({ id: 'far', plan: { start: '2026-12-01', end: '2026-12-02' } }),
        task({ id: 'both', plan: { start: '2026-10-16' }, orchestration: { enabled: true, scheduledAt: new Date(2026, 9, 17, 9).toISOString(), dependencies: [], expectedPaths: [] } }),
        task({ id: 'done', state: 'done', plan: { start: '2026-10-16' } }),
      ],
      NOW,
    );
    expect(g.upcoming.map((u) => u.task.id)).toEqual(['now', 'both', 'soon']);
  });

  it('lists the most recent changes of any state, and not archived work', () => {
    const tasks = [
      task({ id: 'a', updatedAt: '2026-10-14T00:00:00Z', state: 'done' }),
      task({ id: 'b', updatedAt: '2026-10-13T00:00:00Z' }),
      task({ id: 'gone', updatedAt: '2026-10-15T00:00:00Z', archivedAt: 'x' }),
      ...Array.from({ length: 20 }, (_, i) => task({ id: `n${i}`, updatedAt: `2026-09-${String(i + 1).padStart(2, '0')}T00:00:00Z` })),
    ];
    const g = workGroups(tasks, NOW);
    expect(g.recent.slice(0, 2).map((t) => t.id)).toEqual(['a', 'b']);
    expect(g.recent).toHaveLength(15);
    expect(g.recent.some((t) => t.id === 'gone')).toBe(false);
  });
});

describe('upcomingNote', () => {
  const item = (start: string, end: string) => ({ start, end }) as Parameters<typeof upcomingNote>[0];
  it('says under way, or how soon', () => {
    expect(upcomingNote(item('2026-10-14', '2026-10-20'), NOW)).toBe('under way');
    expect(upcomingNote(item('2026-10-15', '2026-10-15'), NOW)).toBe('under way');
    expect(upcomingNote(item('2026-10-16', '2026-10-16'), NOW)).toBe('starts tomorrow');
    expect(upcomingNote(item('2026-10-20', '2026-10-21'), NOW)).toBe('starts in 5 days');
  });
});
