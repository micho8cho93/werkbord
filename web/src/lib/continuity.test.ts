import { describe, expect, it } from 'vitest';
import { approveHold, continuityOf, continueAgents, otherProviders } from './continuity';
import type { Agent, Run } from './types';

const run = (p: Partial<Run>) => ({ id: 'r', taskId: 't', projectId: 'p', agentId: 'claude-code', state: 'failed', ...p }) as Run;
const agent = (id: string, available = true) => ({ id, name: id, installed: true, available }) as Agent;
const quota = { providerKind: 'quota_exhausted', summary: 's', source: 'report', raisedAt: '' } as const;

describe('continuing work', () => {
  it('never treats an unknown stop as stopped, and gives no way past it', () => {
    const c = continuityOf(run({ state: 'running', recovery: 'intervention' }));
    expect(c).toMatchObject({ needsReview: true, stopped: false, stopFirst: false });
    expect(approveHold(c, 'codex')).toContain('stopped');
  });
  it('a session left open by a limit is ended by the same step that continues it', () => {
    const c = continuityOf(run({ state: 'blocked', blocker: quota }));
    expect(c).toMatchObject({ stopped: false, stopFirst: true, providerLimit: true });
    expect(c.headline).toContain('out of allowance');
    expect(approveHold(c, 'codex')).toBe('');
  });
  it('an open session on an unreachable runner cannot be ended from here', () => {
    const c = continuityOf(run({ state: 'blocked', blocker: quota }), false);
    expect(c.stopFirst).toBe(false);
    expect(approveHold(c, 'codex')).toContain('cannot be reached');
    expect(continuityOf(run({ state: 'blocked', remote: true, blocker: quota })).stopFirst).toBe(false);
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
  it('approval needs an agent', () => {
    const c = continuityOf(run({ state: 'failed' }));
    expect(approveHold(c, 'codex')).toBe('');
    expect(approveHold(c, '')).toContain('No other agent');
  });
  it('offers other agents that can start now, and the same agent only when its limit is not the reason', () => {
    const all = [agent('claude-code'), agent('codex'), agent('gemini-cli', false)];
    expect(otherProviders(all, 'claude-code').map(a => a.id)).toEqual(['codex']);
    expect(continueAgents(all, 'claude-code', true).map(a => a.id)).toEqual(['codex']);
    expect(continueAgents(all, 'claude-code', false).map(a => a.id)).toEqual(['codex', 'claude-code']);
  });
});
