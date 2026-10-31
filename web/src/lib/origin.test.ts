import { describe, expect, it } from 'vitest';
import { MAX_CHAIN, chainOf, encodeOrigin, labelOf, parseChain, parseOne, popped, pushed, trail, withChain, type Origin } from './origin';

const all: Origin[] = [
  { kind: 'work' }, { kind: 'agents' }, { kind: 'calendar' }, { kind: 'calendar', projectId: 'prj_a' },
  { kind: 'salie' }, { kind: 'salie', conversation: 'ast_k3j9x2m4q7p1a8z5' }, { kind: 'board', projectId: 'prj_a' },
  { kind: 'plan', projectId: 'prj_a', planId: 'pln_1' }, { kind: 'task', projectId: 'prj_a', taskId: 'tsk_1' },
  { kind: 'session', key: 'run:run_a' }, { kind: 'session', key: 'terminal:abc-1' },
];

describe('an origin names a place and nothing else', () => {
  it('round-trips every kind, session keys included', () => {
    for (const o of all) expect(parseOne(encodeOrigin(o)), encodeOrigin(o)).toEqual(o);
    expect(encodeOrigin({ kind: 'session', key: 'run:run_a' })).toBe('session:run%3Arun_a');
  });
  it('refuses anything that is not exactly one of those shapes', () => {
    for (const bad of ['', 'home', 'work:x', 'agents:x', 'task', 'task:p', 'task:p:t:u', 'task:../x:t', 'task:p:t/u', 'plan:p', 'board:', 'session:task:x',
      'session:run', 'session:run%3A', 'session:run%3A../x', 'calendar:a b', 'salie:' + 'a'.repeat(81), 'javascript:alert(1)', 'https://x.example']) {
      expect(parseOne(bad), bad).toBeNull();
    }
  });
});

describe('a chain is the route back, nearest first', () => {
  it('round-trips through an address with other parts of the query kept', () => {
    const chain: Origin[] = [{ kind: 'session', key: 'run:run_a' }, { kind: 'task', projectId: 'prj_a', taskId: 'tsk_1' }, { kind: 'calendar' }];
    const href = withChain('#/p/prj_a/task/tsk_2?plan=pln_1', chain);
    expect(href.startsWith('#/p/prj_a/task/tsk_2?plan=pln_1&from=')).toBe(true);
    expect(chainOf(href)).toEqual(chain);
    expect(new URLSearchParams(href.split('?')[1]).get('plan')).toBe('pln_1');
    expect(chainOf('#/sessions/run%3Arun_a?project=prj_a&' + href.split('?')[1].split('&').pop())).toEqual(chain);
  });
  it('replaces a chain rather than adding a second, and removes it when empty', () => {
    const once = withChain('#/sessions', [{ kind: 'work' }]);
    expect(withChain(once, [{ kind: 'agents' }])).toBe('#/sessions?from=agents');
    expect(withChain(once, [])).toBe('#/sessions');
  });
  it('is no chain at all if any step is not understood, or it is too long', () => {
    expect(parseChain('work>nonsense')).toEqual([]);
    expect(parseChain(['work', 'work', 'work', 'work'].join('>'))).toEqual([]);
    expect(parseChain('x'.repeat(401))).toEqual([]);
    expect(parseChain(null)).toEqual([]);
    expect(chainOf('#/p/a/task/b?from=%E0%A4%A')).toEqual([]);
    expect(chainOf('#/control')).toEqual([]);
  });
  it('pushes the page it leaves and pops it on the way back, keeping at most MAX_CHAIN steps', () => {
    let chain: Origin[] = [];
    for (const o of [{ kind: 'work' }, { kind: 'plan', projectId: 'p', planId: 'l' }, { kind: 'task', projectId: 'p', taskId: 't' }, { kind: 'session', key: 'run:r' }] as Origin[]) chain = pushed(o, chain);
    expect(chain.map(o => o.kind)).toEqual(['session', 'task', 'plan']);
    expect(chain).toHaveLength(MAX_CHAIN);
    const back = popped(chain)!;
    expect(back.to.kind).toBe('session');
    expect(back.rest.map(o => o.kind)).toEqual(['task', 'plan']);
    expect(popped([])).toBeNull();
    expect(trail(chain).map(o => o.kind)).toEqual(['plan', 'task', 'session']);
  });
  it('drops the oldest steps rather than make an address the window would refuse', () => {
    const long = 'x'.repeat(90);
    const chain: Origin[] = [{ kind: 'task', projectId: long, taskId: long }, { kind: 'task', projectId: long, taskId: long }, { kind: 'task', projectId: long, taskId: long }];
    const href = withChain('#/p/prj_a/task/tsk_1', chain);
    expect(href.length).toBeLessThanOrEqual(280);
    expect(chainOf(href).length).toBeGreaterThan(0);
  });
});

describe('the words for a step', () => {
  it('use what is known and plain words otherwise, never an empty link', () => {
    const names = { task: (_p: string, t: string) => (t === 'tsk_1' ? 'Fix login' : undefined), plan: () => 'Launch' };
    expect(labelOf({ kind: 'task', projectId: 'p', taskId: 'tsk_1' }, names)).toBe('Fix login');
    expect(labelOf({ kind: 'task', projectId: 'p', taskId: 'tsk_9' }, names)).toBe('Task');
    expect(labelOf({ kind: 'plan', projectId: 'p', planId: 'x' }, names)).toBe('Launch');
    expect(labelOf({ kind: 'plan', projectId: 'p', planId: 'x' })).toBe('Plan');
    expect(all.map(o => labelOf(o)).every(Boolean)).toBe(true);
  });
});
