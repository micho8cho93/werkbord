import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const embed = vi.hoisted(() => ({ on: false }));
vi.mock('./embed', () => ({ embedded: () => embed.on }));
const salie = vi.hoisted(() => vi.fn());
vi.mock('./agent-nav', () => ({ backToSalie: salie }));
import { goOrigin, originHref } from './origin-nav';
import { chainOf } from './origin';

const loc = { hash: '' };
const post = vi.fn();
beforeEach(() => { loc.hash = ''; vi.stubGlobal('location', loc); vi.stubGlobal('window', { parent: { postMessage: post } }); });
afterEach(() => { embed.on = false; vi.unstubAllGlobals(); salie.mockClear(); post.mockClear(); });

describe('where Back goes', () => {
  it('is an address wherever the place is an address of this page, carrying the rest of the route', () => {
    const rest = [{ kind: 'calendar' as const, projectId: 'prj_a' }];
    expect(originHref({ kind: 'board', projectId: 'prj_a' }, [])).toBe('#/p/prj_a/work');
    expect(originHref({ kind: 'plan', projectId: 'prj_a', planId: 'pln_1' }, [])).toBe('#/p/prj_a/work?plan=pln_1');
    const task = originHref({ kind: 'task', projectId: 'prj_a', taskId: 'tsk_1' }, rest)!;
    expect(task.startsWith('#/p/prj_a/task/tsk_1?from=')).toBe(true);
    expect(chainOf(task)).toEqual(rest);
    expect(originHref({ kind: 'session', key: 'run:run_a' }, [])).toBe('#/sessions/run%3Arun_a');
    expect(originHref({ kind: 'agents' }, [])).toBe('#/sessions');
    expect(originHref({ kind: 'calendar', projectId: 'prj_a' }, [])).toBe('#/p/prj_a/calendar');
  });
  it('is the window’s page when framed, and Control in a plain browser; Salie is never an address', () => {
    expect(originHref({ kind: 'work' }, [])).toBe('#/control');
    expect(originHref({ kind: 'calendar' }, [])).toBe('#/control');
    embed.on = true;
    expect(originHref({ kind: 'work' }, [])).toBeNull();
    expect(originHref({ kind: 'calendar' }, [])).toBeNull();
    expect(originHref({ kind: 'salie' }, [])).toBeNull();
  });
  it('asks the window for its page, or Salie for the conversation, and otherwise only changes the address', () => {
    embed.on = true;
    goOrigin({ kind: 'work' }, []);
    goOrigin({ kind: 'calendar' }, []);
    expect(post.mock.calls.map(c => (c[0] as { page: string }).page)).toEqual(['inbox', 'calendar']);
    goOrigin({ kind: 'salie', conversation: 'ast_k3j9x2m4q7p1a8z5' }, []);
    expect(salie).toHaveBeenCalledWith('ast_k3j9x2m4q7p1a8z5');
    goOrigin({ kind: 'session', key: 'run:run_a' }, []);
    expect(loc.hash).toBe('#/sessions/run%3Arun_a');
  });
});
