import { describe, expect, it } from 'vitest';
import { reviewCount, reviewRows } from './reviews';
import type { Attention, Overview, WorkItem } from './types';

const entry = (id: string, kind: 'personal' | 'team', attention: Partial<Attention>[], work: Partial<WorkItem>[], answered = true) => {
  const workspace = { id, kind, name: id, state: 'ready' as const };
  return { workspace, ...(answered ? { summary: { schema: 's', workspace, projects: [], work: work as WorkItem[], attention: attention as Attention[], schedule: [], at: '' } } : { error: 'offline' }) };
};
const overview: Overview = { at: '', entries: [
  entry('personal', 'personal', [{ id: 'a1', kind: 'review', severity: 'warning', title: 'Refactor the importer', project: 'side', href: '#/p/1/task/1', at: '2026-10-25T10:00:00Z' }, { id: 'a2', kind: 'needs_input', severity: 'warning', title: 'A question' }], [{ id: 'w1', title: 'Refactor the importer', status: 'review', project: 'side', href: '#/p/1/task/1' }, { id: 'w2', title: 'Add CSV export', status: 'review', project: 'side', href: '#/p/1/task/2', updatedAt: '2026-10-24T10:00:00Z' }, { id: 'w3', title: 'Done thing', status: 'done', project: 'side', href: '#/p/1/task/3' }]),
  entry('team:a', 'team', [{ id: 'b1', kind: 'review', severity: 'warning', title: 'Rate-limit the login API', project: 'web-app', href: '?tab=board&project=w&ticket=t1', at: '2026-10-25T12:00:00Z' }, { id: 'b2', kind: 'changes_requested', severity: 'warning', title: 'Order history page', project: 'web-app', href: '?tab=board&project=w&ticket=t2', at: '2026-10-25T09:00:00Z' }], []),
  entry('team:b', 'team', [{ id: 'c1', kind: 'review', severity: 'warning', title: 'Never shown: it did not answer' }], [], false),
] };

describe('the one list of reviews', () => {
  it('gathers reviews from Personal and every team that answered, most urgent kind first, newest first within a kind', () => {
    expect(reviewRows(overview).map((r) => [r.workspace.id, r.kind, r.title])).toEqual([
      ['team:a', 'to_review', 'Rate-limit the login API'],
      ['personal', 'to_review', 'Refactor the importer'],
      ['team:a', 'changes', 'Order history page'],
      ['personal', 'mine', 'Add CSV export'],
    ]);
  });

  it('does not list the same piece of work twice, and leaves out what is not a review', () => {
    const titles = reviewRows(overview).map((r) => r.title);
    expect(titles.filter((t) => t === 'Refactor the importer')).toHaveLength(1);
    expect(titles).not.toContain('A question');
    expect(titles).not.toContain('Done thing');
  });

  it('filters by place: Personal only, or one team', () => {
    expect(reviewRows(overview, 'personal').map((r) => r.workspace.id)).toEqual(['personal', 'personal']);
    expect(reviewRows(overview, 'team:a')).toHaveLength(2);
    expect(reviewRows(overview, 'team:b')).toEqual([]);
  });

  it('counts only what waits on the person', () => {
    expect(reviewCount(overview)).toBe(3);
    expect(reviewCount(null)).toBe(0);
  });
});
