import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const embed = vi.hoisted(() => ({ on: false }));
vi.mock('./embed', () => ({ embedded: () => embed.on }));
const hide = vi.hoisted(() => vi.fn());
vi.mock('./assistant/sessions.svelte', () => ({ assistant: { hide, show: vi.fn(), openConversation: vi.fn() } }));
const request = vi.hoisted(() => vi.fn());
vi.mock('./api', () => ({ request, api: new Proxy({}, { get: () => request }) }));
import { openAgents } from './agent-nav';
import { chainOf } from './origin';

const loc = { hash: '' };
const post = vi.fn();
beforeEach(() => { loc.hash = ''; vi.stubGlobal('location', loc); vi.stubGlobal('window', { parent: { postMessage: post } }); });
afterEach(() => { embed.on = false; vi.unstubAllGlobals(); post.mockClear(); request.mockClear(); hide.mockClear(); });

describe('opening a session is only ever a move to where it already is', () => {
  const from = [{ kind: 'task' as const, projectId: 'prj_a', taskId: 'tsk_1' }, { kind: 'calendar' as const }];
  it('in a plain browser sets the address, with the route back, and calls nothing', async () => {
    openAgents('run:run_a', from);
    expect(loc.hash.startsWith('#/sessions/run%3Arun_a?from=')).toBe(true);
    expect(chainOf(loc.hash)).toEqual(from);
    await vi.waitFor(() => expect(hide).toHaveBeenCalled());
    expect(request).not.toHaveBeenCalled();
  });
  it('in the window asks it to show the session, naming the route back, and calls nothing', () => {
    embed.on = true;
    openAgents('terminal:abc-1', from);
    expect(post).toHaveBeenCalledExactlyOnceWith({ type: 'werkbord.frame', event: 'agent_session', key: 'terminal:abc-1', from: 'task:prj_a:tsk_1>calendar' }, '*');
    expect(loc.hash).toBe('');
    expect(request).not.toHaveBeenCalled();
  });
  it('is the same address and message for the same session however many times it is opened', () => {
    embed.on = true;
    openAgents('run:run_a'); openAgents('run:run_a');
    expect(post.mock.calls[0]).toEqual(post.mock.calls[1]);
    expect(post.mock.calls[0][0]).toEqual({ type: 'werkbord.frame', event: 'agent_session', key: 'run:run_a' });
  });
  it('refuses anything that is not a session key', () => {
    for (const bad of ['run:../x', 'task:tsk_1', 'run:', '']) { openAgents(bad, from); }
    expect(post).not.toHaveBeenCalled();
    expect(loc.hash).toBe('');
  });
});
