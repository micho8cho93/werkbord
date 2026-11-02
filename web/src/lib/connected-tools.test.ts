import { describe, expect, it } from 'vitest';
import { codingAgents, connectedTools, usageLines, USAGE_CAVEAT } from './connected-tools';

describe('Agents & connections says only what was detected', () => {
  it('lists agents as Signed in or Installed with their own live sessions', () => {
    const rows = codingAgents([{ id: 'codex', name: 'Codex', signedIn: true, active: 1, limitEvents: 0 }, { id: 'claude-code', name: 'Claude Code', signedIn: false, active: 0, limitEvents: 0 }]);
    expect(rows.map(r => [r.id, r.status, r.active, r.limit])).toEqual([['codex', 'Signed in', 1, 'Limit unknown'], ['claude-code', 'Installed', 0, 'Limit unknown']]);
  });
  it('reports a reached limit only when the usage says so', () => {
    expect(codingAgents([{ id: 'codex', name: 'Codex', signedIn: true, active: 0, limitEvents: 2 }])[0].limit).toBe('Limit reached 2 times this period');
    expect(codingAgents([{ id: 'codex', name: 'Codex', signedIn: true, active: 0, limitEvents: 1 }])[0].limit).toBe('Limit reached 1 time this period');
  });
  it('words a tool by its test, and finds the mark of a known service', () => {
    const rows = connectedTools([{ id: 'a', name: 'Notion', service: 'https://mcp.notion.com/mcp', state: 'verified' }, { id: 'b', name: 'Mine', state: 'untested' }, { id: 'c', name: 'Broken', state: 'failed' }]);
    expect(rows.map(r => r.state)).toEqual(['Verified', 'Not yet tested', 'Last test failed']);
    expect(rows[0].service?.id).toBe('notion');
    expect(rows[1].service).toBeUndefined();
  });
  it('keeps reported charges and the estimate apart and renders no allowance, balance or reset', () => {
    // A reading that carries fields it should not: they must not come through.
    const lines = usageLines({ reported: 1.25, estimated: null, remaining: 40, remainingAllowance: '60%', resetAt: '2026-11-05', balance: 3 } as never)!;
    expect(Object.keys(lines).sort()).toEqual(['caveat', 'estimated', 'reported']);
    expect(lines.reported).toBe('$1.25');
    expect(lines.estimated).toBe('Not reported');
    expect(lines.caveat).toBe(USAGE_CAVEAT);
    expect(JSON.stringify({ ...lines, caveat: '' })).not.toMatch(/remain|reset|balance|40|60%/i);
    expect(usageLines(null)).toBeUndefined();
  });
});
