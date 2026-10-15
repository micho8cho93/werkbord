import { describe, expect, it } from 'vitest';
import { agentCards, agentSessions, fleet, fleetLine, matchesFilter, usageLine } from './agents';
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

describe('agentCards', () => {
  const overview = { runs: [run('a', 'running', 'codex'), run('c', 'waiting_for_user', 'claude', 'question'), run('r', 'running', 'runner-only')] } as unknown as Overview;
  const cards = agentCards([agent('codex'), agent('claude'), agent('gemini'), agent('off', false)], overview);

  it('has a card for each agent, including one only a runner has, the ones that need the person first', () => {
    expect(cards.map((c) => [c.id, c.state])).toEqual([
      ['claude', 'Waiting for you'],
      ['codex', 'Working'],
      ['runner-only', 'Working'],
      ['gemini', 'Idle'],
      ['off', 'Not available'],
    ]);
  });

  it('carries each agent’s own active sessions', () => {
    expect(cards.find((c) => c.id === 'codex')!.sessions.map((x) => x.run.id)).toEqual(['a']);
    expect(cards.find((c) => c.id === 'gemini')!.sessions).toEqual([]);
  });

  it('filters and says how the fleet stands', () => {
    expect(cards.filter((c) => matchesFilter(c, 'running')).map((c) => c.id)).toEqual(['codex', 'runner-only']);
    expect(cards.filter((c) => matchesFilter(c, 'waiting')).map((c) => c.id)).toEqual(['claude']);
    expect(cards.filter((c) => matchesFilter(c, 'idle')).map((c) => c.id)).toEqual(['gemini']);
    expect(cards.filter((c) => matchesFilter(c, 'all'))).toHaveLength(5);
    expect(fleetLine(cards)).toBe('2 running · 1 waiting for you · 1 idle');
    expect(fleetLine([])).toBe('None running');
  });
});
