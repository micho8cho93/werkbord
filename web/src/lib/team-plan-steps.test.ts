import { describe, expect, it } from 'vitest';
import '../../../internal/uipresentation/static/work-queue.js';

const k = (id: string, o: Record<string, unknown> = {}) => ({ id, title: 'Step ' + id, status: 'available', ...o });
const names: Record<string, string> = { maya: 'Maya', lucas: 'Lucas' };
const steps = (list: ReturnType<typeof k>[], me = 'maya') => WerkbordWork.planSteps(list, me, (id) => names[id]);

describe('a Team plan from one member\'s side', () => {
  it('offers the first of my own steps that is ready, and nobody else\'s', () => {
    const r = steps([k('a', { assigneeId: 'lucas', status: 'in_progress' }), k('b', { assigneeId: 'maya', status: 'in_progress' }), k('c', { assigneeId: 'maya' })]);
    expect(r.steps.map(s => s.state)).toEqual(['theirs', 'ready', 'ready']);
    expect(r.next).toBe('b');
    expect(r.mineOpen).toBe(2);
  });
  it('a step waits for its dependencies, and names who holds them', () => {
    const r = steps([k('a', { assigneeId: 'lucas', status: 'in_progress' }), k('b', { assigneeId: 'maya', dependencies: ['a'] })]);
    expect(r.steps[1]).toMatchObject({ state: 'waiting', canStart: false, waitsFor: [{ number: 1, holder: 'Lucas' }] });
    expect(r.next).toBeNull();
    expect(r.mineWaiting).toBe(1);
  });
  it('a finished dependency no longer holds a step back', () => {
    const r = steps([k('a', { assigneeId: 'lucas', status: 'done' }), k('b', { assigneeId: 'maya', dependencies: ['a'] })]);
    expect(r.steps[1]).toMatchObject({ state: 'ready', canStart: true });
    expect(r.next).toBe('b');
  });
  it('the plan order informs but does not block', () => {
    const r = steps([k('a', { assigneeId: 'lucas', status: 'in_progress' }), k('b', { assigneeId: 'maya' })]);
    expect(r.steps[1]).toMatchObject({ state: 'ready', follows: { number: 1, holder: 'Lucas' } });
  });
  it('does not offer human work, work under review, unclaimed work or a step already running', () => {
    const r = steps([
      k('a', { assigneeId: 'maya', workMode: 'human' }),
      k('b', { assigneeId: 'maya', status: 'review' }),
      k('c'),
      k('d', { assigneeId: 'maya', status: 'in_progress', running: true }),
    ]);
    expect(r.steps.map(s => s.state)).toEqual(['yours-human', 'review', 'unclaimed', 'running']);
    expect(r.next).toBeNull();
    expect(r.unclaimed).toBe(1);
  });
  it('leaves archived steps out and numbers the rest', () => {
    const r = steps([k('a', { archived: true }), k('b', { assigneeId: 'maya' })]);
    expect(r.steps).toHaveLength(1);
    expect(r.steps[0].number).toBe(1);
  });
  it('names a former member rather than guessing', () => {
    expect(steps([k('a', { assigneeId: 'gone', status: 'in_progress' })]).steps[0].holder).toBe('a former member');
  });
});
