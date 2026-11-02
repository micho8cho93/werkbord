import { describe, expect, it } from 'vitest';
import { approveHold, continuityOf, otherProviders } from './continuity';
import type { Agent, Run } from './types';

const run = (p: Partial<Run>) => ({ id: 'r', taskId: 't', projectId: 'p', agentId: 'claude-code', state: 'failed', ...p }) as Run;
const agent = (id: string, available = true) => ({ id, name: id, installed: true, available }) as Agent;

describe('provider continuity', () => {
  it('never treats an unknown stop as stopped', () => {
    const c = continuityOf(run({ state: 'running', recovery: 'intervention' }));
    expect(c).toMatchObject({ needsReview: true, stopped: false });
    expect(approveHold(c, true, 'codex')).toContain('stopped');
  });
  it('a blocked session is still open', () => {
    const c = continuityOf(run({ state: 'blocked', blocker: { providerKind: 'quota_exhausted', summary: 's', source: 'report', raisedAt: '' } }));
    expect(c.stopped).toBe(false);
    expect(c.headline).toContain('out of allowance');
  });
  it('a failed run is stopped only while its runner is reachable', () => {
    expect(continuityOf(run({ state: 'failed' })).stopped).toBe(true);
    const c = continuityOf(run({ state: 'failed' }), false);
    expect(c.stopped).toBe(false);
    expect(c.hold).toContain('not confirmed');
  });
  it('a plain stop by the person needs no decision, and live or finished runs none either', () => {
    expect(continuityOf(run({ state: 'stopped' })).needsReview).toBe(false);
    expect(continuityOf(run({ state: 'running' })).needsReview).toBe(false);
    expect(continuityOf(run({ state: 'completed' })).needsReview).toBe(false);
  });
  it('approval needs a confirmed stop, a handoff and another provider', () => {
    const c = continuityOf(run({ state: 'failed' }));
    expect(approveHold(c, true, 'codex')).toBe('');
    expect(approveHold(c, false, 'codex')).toContain('handoff');
    expect(approveHold(c, true, '')).toContain('No other agent');
  });
  it('offers only other agents that can start now', () => {
    expect(otherProviders([agent('claude-code'), agent('codex'), agent('gemini-cli', false)], 'claude-code').map(a => a.id)).toEqual(['codex']);
  });
});
