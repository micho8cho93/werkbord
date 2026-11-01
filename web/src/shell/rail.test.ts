import { describe, expect, it } from 'vitest';
import { railBadges } from './rail';
import type { Attention, Overview } from './types';

const item = (kind: string, id = kind): Attention => ({ id, kind, severity: 'warning', title: kind });
const overview = (...attention: Attention[]): Overview => ({
  at: '', entries: [{ workspace: { id: 'personal', kind: 'personal', name: 'Personal', state: 'ready' } as never, summary: { attention, work: [] } as never }],
});

describe('the sidebar badges', () => {
  it('say nothing when nothing waits', () => {
    expect(railBadges(null)).toEqual({});
    expect(railBadges(overview())).toEqual({ inbox: undefined, agents: undefined });
  });

  it('take the worst kind of wait as the tone, and count them all', () => {
    const b = railBadges(overview(item('review', 'r1'), item('failed'), item('needs_input')));
    expect(b.inbox).toMatchObject({ count: 3, tone: 'ask' });
    expect(b.inbox?.label).toContain('1 question waiting for an answer');
    expect(railBadges(overview(item('review'), item('failed'))).inbox).toMatchObject({ count: 2, tone: 'bad' });
    expect(railBadges(overview(item('review'))).inbox).toMatchObject({ count: 1, tone: 'review' });
  });

  it('keep reviews out of Agents, which holds sessions', () => {
    expect(railBadges(overview(item('review'))).agents).toBeUndefined();
    expect(railBadges(overview(item('failed'))).agents).toMatchObject({ count: 1, tone: 'bad' });
  });
});
