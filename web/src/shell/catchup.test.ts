import { describe, expect, it } from 'vitest';
import { recap, readVisit } from './catchup';
import type { Change, Overview } from './types';
const since = Date.parse('2026-01-01T00:00:00Z');
const through = since + 86_400_000;
const change = (key: string, origin?: string): Change => ({ id: key, key, title: key, kind: 'completed', href: '#/control', at: new Date(since + 1000).toISOString(), origin });
function overview(personal: Change[], team: Change[]): Overview {
  return { at: new Date(through).toISOString(), entries: ['personal', 'team:w'].map((id, i) => {
    const workspace = { id, kind: i ? 'team' as const : 'personal' as const, name: id, state: 'ready' as const };
    return { workspace, summary: { schema: 'werkbord.workspace/v1', workspace, projects: [], work: [], attention: [], schedule: [], at: new Date(through).toISOString(), changes: { since: new Date(since).toISOString(), through: new Date(through).toISOString(), cursor: 5, incomplete: false, items: i ? team : personal } } };
  }) };
}
describe('measured catch-up', () => {
  it('deduplicates reports and linked local handoffs', () => {
    const o = overview([change('local', 'werkbord-source:v1:w:p:t:m')], [change('ticket:w:p:t'), change('ticket:w:p:t')]);
    expect(recap(o, { at: since, cursors: {} }, through).items.map(i => i.change.key)).toEqual(['ticket:w:p:t']);
  });
  it('never includes events after the frozen interval or revoked workspace content', () => {
    const o = overview([change('local')], [change('team')]);
    o.entries[0].summary!.changes!.items[0].at = new Date(through + 1).toISOString();
    o.entries[1].error = 'permission denied';
    expect(recap(o, { at: since, cursors: {} }, through).items).toEqual([]);
    expect(recap(o, { at: since, cursors: {} }, through).unavailable).toEqual(['team:w']);
  });
  it('preserves an offline workspace checkpoint instead of losing its activity', () => {
    const o = overview([], [change('team')]);
    expect(recap(o, { at: through, cursors: {}, coveredAt: { 'team:w': since } }, through).items).toHaveLength(1);
  });
  it('rejects corrupt or future visit records', () => {
    for (const raw of ['broken', '{}', JSON.stringify({ at: Date.now() + 10000, cursors: {} }), JSON.stringify({ at: since, cursors: { p: -1 } })]) expect(readVisit({ getItem: () => raw })).toBeNull();
  });
});
