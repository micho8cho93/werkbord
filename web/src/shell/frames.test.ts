import { describe, expect, it, vi } from 'vitest';
import { handle, isPlace, surfaceUrl, type FrameRef, type Deps } from './frames';

const individual = {} as Window, team = {} as Window, assistantWindow = {} as Window;
const frames: FrameRef[] = [{ id: 'personal', origin: 'http://127.0.0.1:7420', window: individual }, { id: 'team:main', origin: 'http://127.0.0.1:7431', window: team }];
// The assistant's own frame: a page of the person's own Werkbord, which is not one of the workspaces’ frames.
const surface: FrameRef = { id: 'personal', surface: true, origin: 'http://127.0.0.1:7420', window: assistantWindow };
const deps = (): Deps => ({ relay: vi.fn().mockResolvedValue('ok'), reply: vi.fn(), remember: vi.fn().mockResolvedValue(undefined), ready: vi.fn(), open: vi.fn(), assistant: vi.fn() });
const data = { type: 'werkbord.native.request', id: 'request1', method: 'Info', args: [] };

describe('frame authority', () => {
  it('rejects forged sources, origins and invalid calls without relay', async () => {
    const d = deps();
    for (const e of [
      { source: {}, origin: frames[0].origin, data },
      { source: individual, origin: frames[1].origin, data },
      { source: team, origin: 'https://evil.example', data },
      { source: team, origin: frames[1].origin, data: { ...data, args: ['x'.repeat(8193)] } },
      { source: team, origin: frames[1].origin, data: { ...data, method: 'eval()' } },
    ]) await handle(frames, e, d);
    expect(d.relay).not.toHaveBeenCalled();
  });
  it('uses the shell identity even if a Team frame claims to be Individual', async () => {
    const d = deps();
    await handle(frames, { source: team, origin: frames[1].origin, data: { ...data, workspace: 'personal' } }, d);
    expect(d.relay).toHaveBeenCalledWith('team:main', 'Info', []);
    expect(d.reply).toHaveBeenCalledWith(frames[1], { type: 'werkbord.native.result', id: 'request1', ok: true, result: 'ok' });
  });
  it('remembers only internal routes from known frames', async () => {
    const d = deps();
    for (const place of ['https://evil.example', '?tab=//evil', '?tab=../personal', '?token=SECRET', '?tab=board&join=SECRET', '#/p/prj_1?invite=SECRET', '?%74oken=SECRET', '?tab=board&project=tpj_1']) await handle(frames, { source: team, origin: frames[1].origin, data: { type: 'werkbord.frame', event: 'place', place } }, d);
    expect(d.remember).toHaveBeenCalledExactlyOnceWith('team:main', '?tab=board&project=tpj_1');
    expect(isPlace('#/p/prj_1/task/tsk_1')).toBe(true);
  });
  it('lets a frame open only a place in the person’s own Werkbord', async () => {
    const d = deps();
    for (const [target, place] of [['team:main', '#/control'], ['personal', 'https://evil.example'], ['personal', '?tab=board'], ['personal', '#/p/x?token=SECRET'], ['personal', '#//evil']]) await handle(frames, { source: team, origin: frames[1].origin, data: { type: 'werkbord.frame', event: 'open', target, place } }, d);
    expect(d.open).not.toHaveBeenCalled();
    await handle(frames, { source: team, origin: frames[1].origin, data: { type: 'werkbord.frame', event: 'open', target: 'personal', place: '#/p/prj_1/task/tsk_1' } }, d);
    expect(d.open).toHaveBeenCalledExactlyOnceWith('team:main', 'personal', '#/p/prj_1/task/tsk_1');
  });
});

describe('the assistant report', () => {
  const say = (data: Record<string, unknown>, from: Window = assistantWindow, origin = surface.origin) => ({ source: from, origin, data: { type: 'werkbord.frame', event: 'assistant', ...data } });
  it('takes a count and two flags from the assistant’s own frame', async () => {
    const d = deps();
    await handle([surface], say({ open: true, waiting: 2, running: false }), d);
    expect(d.assistant).toHaveBeenCalledWith('personal', { open: true, waiting: 2, running: false });
  });
  it('ignores the workspace’s page, a Team frame, a forged origin and anything that is not exactly that shape', async () => {
    const d = deps();
    const all = [...frames, surface];
    await handle(all, say({ open: true, waiting: 1, running: false }, individual, frames[0].origin), d); // the workspace page is not the assistant
    await handle(all, say({ open: true, waiting: 1, running: false }, team, frames[1].origin), d);
    await handle(all, say({ open: true, waiting: 1, running: false }, assistantWindow, 'https://evil.example'), d);
    for (const bad of [{ open: 'yes', waiting: 1, running: false }, { open: true, waiting: -1, running: false }, { open: true, waiting: 1.5, running: false }, { open: true, waiting: 100, running: false }, { open: true, waiting: '3', running: false }, { open: true, waiting: 1 }]) {
      await handle(all, say(bad), d);
    }
    expect(d.assistant).not.toHaveBeenCalled();
  });
  it('lets the assistant’s frame ask the app for things as the person’s own Werkbord may, and do nothing else a workspace page does', async () => {
    const d = deps();
    await handle([surface], { source: assistantWindow, origin: surface.origin, data: { ...data, method: 'ChooseFiles' } }, d);
    expect(d.relay).toHaveBeenCalledExactlyOnceWith('personal', 'ChooseFiles', []);
    for (const event of [{ event: 'ready' }, { event: 'place', place: '#/control' }]) {
      await handle([surface], { source: assistantWindow, origin: surface.origin, data: { type: 'werkbord.frame', ...event } }, d);
    }
    expect(d.ready).not.toHaveBeenCalled();
    expect(d.remember).not.toHaveBeenCalled();
    expect(d.open).not.toHaveBeenCalled();
  });
  it('lets the assistant’s frame show a place in the person’s own Werkbord, and nothing but a place', async () => {
    const d = deps();
    for (const [target, place] of [['personal', 'https://evil.example'], ['personal', '?tab=board'], ['personal', '#/p/x?token=SECRET'], ['personal', '#//evil'], ['personal', '#/p/../x'], ['personal', '#/p/x?join=1'], ['other', '#/deck']]) {
      await handle([surface], { source: assistantWindow, origin: surface.origin, data: { type: 'werkbord.frame', event: 'open', target, place } }, d);
    }
    expect(d.open).not.toHaveBeenCalled();
    await handle([surface], { source: assistantWindow, origin: surface.origin, data: { type: 'werkbord.frame', event: 'open', target: 'personal', place: '#/p/prj_1/git/branch/local/devboard%2Ffix' } }, d);
    expect(d.open).toHaveBeenCalledExactlyOnceWith('personal', 'personal', '#/p/prj_1/git/branch/local/devboard%2Ffix');
  });
  it('can open a validated ticket route in a Team workspace', async () => {
    const d = deps();
    await handle([surface], { source: assistantWindow, origin: surface.origin, data: { type: 'werkbord.frame', event: 'open', target: 'team:main', place: '?tab=board&project=p_1&ticket=t_1' } }, d);
    expect(d.open).toHaveBeenCalledExactlyOnceWith('personal', 'team:main', '?tab=board&project=p_1&ticket=t_1');
    await handle([surface], { source: assistantWindow, origin: surface.origin, data: { type: 'werkbord.frame', event: 'open', target: 'team:main', place: 'https://evil.example' } }, d);
    expect(d.open).toHaveBeenCalledExactlyOnceWith('personal', 'team:main', '?tab=board&project=p_1&ticket=t_1');
  });
  it('relays the six bound arguments required for an approved Team task', async () => {
    const d = deps();
    const args = ['ast_1', 'wreq_1', 'act_1', 'team:main', 'create_ticket', { projectId: 'p_1', title: 'Prepare launch' }];
    await handle([surface], { source: assistantWindow, origin: surface.origin, data: { ...data, method: 'AssistantWorkspaceCall', args } }, d);
    expect(d.relay).toHaveBeenCalledExactlyOnceWith('personal', 'AssistantWorkspaceCall', args);
  });
});

describe('where the assistant’s frame loads from', () => {
  it('is the person’s own Werkbord, asked to show the assistant alone, with the sign-in kept in the fragment', () => {
    expect(surfaceUrl('http://127.0.0.1:7420/#token=abc%2F1')).toBe('http://127.0.0.1:7420/?surface=assistant#token=abc%2F1');
    expect(surfaceUrl('http://127.0.0.1:7420/')).toBe('http://127.0.0.1:7420/?surface=assistant');
  });
});
