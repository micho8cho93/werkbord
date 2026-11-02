import { describe, expect, it } from 'vitest';
import { fallbackLine, fallbackProblem, orderedChoices } from './fallback';

const name = (id: string) => id.toUpperCase();
describe('automatic fallback', () => {
  it('words each outcome without claiming more than was recorded', () => {
    expect(fallbackLine({ runId: 'r', taskId: 't', agent: 'codex', result: 'started', at: '' }, name)).toBe('Continued with CODEX.');
    expect(fallbackLine({ runId: 'r', taskId: 't', agent: 'codex', result: 'declined', reason: 'not continued automatically: no recorded work', at: '' }, name)).toBe('Not continued with CODEX: no recorded work.');
    expect(fallbackLine({ runId: 'r', taskId: 't', agent: 'codex', result: 'claimed', at: '' }, name)).toContain('Nothing is known to have started');
  });
  it('keeps the chosen order and refuses an enabled setting with no agent', () => {
    expect(orderedChoices(['a', 'b', 'c'], ['c', 'a', 'gone'])).toEqual(['c', 'a', 'b']);
    expect(fallbackProblem({ enabled: true, providers: [], maxSwitches: 1, dailyLimit: 3 })).toContain('at least one');
    expect(fallbackProblem({ enabled: false, providers: [], maxSwitches: 1, dailyLimit: 3 })).toBe('');
    expect(fallbackProblem({ enabled: false, providers: [], maxSwitches: 4, dailyLimit: 3 })).toContain('1 to 3');
  });
});
