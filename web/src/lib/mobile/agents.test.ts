import { describe, expect, it } from 'vitest';
import { agentSessions, fleet, usageLine } from './agents';
import type { Agent, Overview, Run } from '../types';

const run = (id: string, state: Run['state'], agentId = 'codex', waiting?: Run['waiting']) => ({ run: { id, state, agentId, waiting } as Run, projectName: 'P', taskTitle: id });
const agent = (id: string, available = true) => ({ id, name: id, installed: true, available }) as Agent;

describe('agentSessions', () => {
  const overview = { runs: [run('a', 'running'), run('b', 'starting'), run('c', 'waiting_for_user', 'codex', 'question'), run('d', 'blocked')], failed: [run('e', 'failed')] } as unknown as Overview;
  it('sorts runs into working, waiting for the person and failed', () => {
    const s = agentSessions(overview);
    expect(s.running.map((x) => x.run.id)).toEqual(['a', 'b']);
    expect(s.needs.map((x) => x.run.id)).toEqual(['c', 'd']);
    expect(s.failed.map((x) => x.run.id)).toEqual(['e']);
  });
  it('is empty before anything is known', () => {
    expect(agentSessions(null)).toEqual({ needs: [], running: [], failed: [] });
  });
});

describe('fleet', () => {
  const overview = { runs: [run('a', 'running', 'codex'), run('c', 'waiting_for_user', 'claude', 'question')] } as unknown as Overview;
  it('says what each agent is doing, and idle only when it is ready and doing nothing', () => {
    const f = fleet([agent('codex'), agent('claude'), agent('gemini'), agent('other', false)], overview);
    expect(f.map((e) => [e.agent.id, e.state, e.active])).toEqual([
      ['codex', 'Working', 1],
      ['claude', 'Waiting for you', 1],
      ['gemini', 'Idle', 0],
      ['other', 'Not available', 0],
    ]);
  });
});

describe('usageLine', () => {
  it('writes what the agent reported and nothing it did not', () => {
    expect(usageLine({ usage: { inputTokens: 12345, outputTokens: 987 } })).toBe('12k in · 987 out');
    expect(usageLine({ usage: { inputTokens: 1500 } })).toBe('1.5k in');
    expect(usageLine({})).toBe('');
    expect(usageLine({ usage: {} })).toBe('');
  });
});
