import { describe, expect, it, vi } from 'vitest';
import { handle, hostedUrl, isPlace, surfaceUrl, type FrameRef, type Deps } from './frames';

const individual = {} as Window, team = {} as Window, assistantWindow = {} as Window;
const frames: FrameRef[] = [{ id: 'personal', origin: 'http://127.0.0.1:7420', window: individual }, { id: 'team:main', origin: 'http://127.0.0.1:7431', window: team }];
// The assistant's own frame: a page of the person's own Werkbord, which is not one of the workspaces’ frames.
const surface: FrameRef = { id: 'personal', surface: true, origin: 'http://127.0.0.1:7420', window: assistantWindow };
const deps = (): Deps => ({ relay: vi.fn().mockResolvedValue('ok'), reply: vi.fn(), remember: vi.fn().mockResolvedValue(undefined), ready: vi.fn(), open: vi.fn(), assistant: vi.fn(), theme: vi.fn(), notify: vi.fn(), settings: vi.fn() });
const data = { type: 'werkbord.native.request', id: 'request1', method: 'Info', args: [] };

const hosted: FrameRef = { id: 'personal', hosted: true, origin: 'http://127.0.0.1:7420', window: {} as Window };
const say = (source: Window, origin: string, data: Record<string, unknown>) => ({ source, origin, data: { type: 'werkbord.frame', ...data } });

describe('what a frame may say about the window', () => {
  it('lets only the main Individual frame read or change validated Salie placement preferences', async () => {
    const value = { universal: 'both', workspace: 'sidebar' } as const;
    const placement = vi.fn().mockReturnValue(value);
    const d = { ...deps(), saliePlacement: placement };
    const request = { event: 'salie_placement', action: 'set', value };
    for (const frame of [frames[1], hosted, surface]) await handle([frame], say(frame.window!, frame.origin, request), d);
    await handle(frames, say(individual, 'https://other.example', request), d);
    for (const bad of [null, {}, { universal: 'none', workspace: 'sidebar' }, { universal: 'both', workspace: 1 }]) {
      await handle(frames, say(individual, frames[0].origin, { ...request, value: bad }), d);
    }
    expect(placement).not.toHaveBeenCalled();
    await handle(frames, say(individual, frames[0].origin, { event: 'salie_placement', action: 'get' }), d);
    expect(placement).toHaveBeenLastCalledWith(undefined);
    await handle(frames, say(individual, frames[0].origin, request), d);
    expect(placement).toHaveBeenLastCalledWith(value);
    expect(d.reply).toHaveBeenLastCalledWith(frames[0], { type: 'werkbord.salie-placement', value });
  });
  it('may say which theme the person chose, and nothing else about it', async () => {
    const d = deps();
    for (const theme of ['dark', 'light', '']) await handle(frames, say(individual, frames[0].origin, { event: 'theme', theme }), d);
    for (const theme of ['blue', 1, null, undefined]) await handle(frames, say(individual, frames[0].origin, { event: 'theme', theme }), d);
    expect(vi.mocked(d.theme).mock.calls).toEqual([['personal', 'dark'], ['personal', 'light'], ['personal', '']]);
  });
  it('may say which style the person chose, and nothing else about it', async () => {
    const d = { ...deps(), style: vi.fn() };
    for (const style of ['paper', 'graphite', 'classic']) await handle(frames, say(individual, frames[0].origin, { event: 'style', style }), d);
    for (const style of ['', 'neon', 1, null, undefined]) await handle(frames, say(individual, frames[0].origin, { event: 'style', style }), d);
    expect(vi.mocked(d.style).mock.calls).toEqual([['personal', 'paper'], ['personal', 'graphite'], ['personal', 'classic']]);
  });
  it('lets Individual say something happened, in plain short words, and no one else', async () => {
    const d = deps();
    const alert = { event: 'notify', key: 'seq-12', title: 'Needs you', body: 'Shop · Fix login' };
    await handle(frames, say(team, frames[1].origin, alert), d);
    await handle([surface], say(assistantWindow, surface.origin, alert), d);
    expect(d.notify).not.toHaveBeenCalled();
    for (const bad of [{ ...alert, key: 'a b' }, { ...alert, title: '' }, { ...alert, title: 'x'.repeat(81) }, { ...alert, body: 7 }, { ...alert, key: undefined }]) await handle(frames, say(individual, frames[0].origin, bad), d);
    expect(d.notify).not.toHaveBeenCalled();
    await handle(frames, say(individual, frames[0].origin, { ...alert, title: 'Needs\u0007 you', urgent: true }), d);
    expect(d.notify).toHaveBeenCalledExactlyOnceWith('personal', { key: 'seq-12', title: 'Needs you', body: 'Shop · Fix login', urgent: true });
  });
  it('lets a workspace’s page ask for Settings only at a place of its own settings', async () => {
    const d = deps();
    for (const place of ['#/control', '#/settings//x', 'https://evil.example', '?tab=settings']) await handle(frames, say(individual, frames[0].origin, { event: 'settings', place }), d);
    for (const place of ['#/settings?phones', '?tab=//x', '#/settings']) await handle(frames, say(team, frames[1].origin, { event: 'settings', place }), d);
    expect(d.settings).not.toHaveBeenCalled();
    await handle(frames, say(individual, frames[0].origin, { event: 'settings', place: '#/settings?phones' }), d);
    await handle(frames, say(team, frames[1].origin, { event: 'settings', place: '?tab=devices' }), d);
    expect(vi.mocked(d.settings).mock.calls).toEqual([['personal', '#/settings?phones'], ['team:main', '?tab=devices']]);
  });
  it('does not take a frame that only shows Settings or Help for where the person is', async () => {
    const d = deps();
    const win = hosted.window as Window;
    await handle([hosted], say(win, hosted.origin, { event: 'place', place: '#/settings?phones' }), d);
    await handle([hosted], say(win, hosted.origin, { event: 'settings', place: '#/settings?phones' }), d);
    expect(d.remember).not.toHaveBeenCalled();
    expect(d.settings).not.toHaveBeenCalled();
    await handle([hosted], say(win, hosted.origin, { event: 'theme', theme: 'dark' }), d);
    expect(d.theme).toHaveBeenCalledOnce();
  });
});

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
    for (const bad of [{ open: 'yes', waiting: 1, running: false }, { open: true, waiting: -1, running: false }, { open: true, waiting: 1.5, running: false }, { open: true, waiting: Number.MAX_SAFE_INTEGER + 1, running: false }, { open: true, waiting: '3', running: false }, { open: true, waiting: 1 }]) {
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

describe('where a frame that shows Settings loads from', () => {
  it('is the same page, told it is not the workspace on show', () => {
    expect(hostedUrl('http://127.0.0.1:7420/#token=x')).toBe('http://127.0.0.1:7420/?hosted=1#token=x');
    expect(hostedUrl('http://127.0.0.1:7431/w/two/?tab=board')).toBe('http://127.0.0.1:7431/w/two/?tab=board&hosted=1');
  });
});

it('accepts setup status only from the ordinary Individual workspace with validated booleans', async () => {
  const onboarding = vi.fn();
  const d = {...deps(), onboarding};
  const report = {event:'onboarding', required:true, paused:false};
  await handle(frames, say(individual, frames[0].origin, report), d);
  expect(onboarding).toHaveBeenCalledExactlyOnceWith(true, false);
  onboarding.mockClear();
  for (const [frame, data] of [[frames[1], report], [hosted, report], [surface, report], [frames[0], {...report, required:'true'}], [frames[0], {...report, paused:null}]] as const) {
    await handle([frame], say(frame.window!, frame.origin, data), d);
  }
  expect(onboarding).not.toHaveBeenCalled();
});

it('opens a personal workspace draft only from the ordinary Individual frame', async () => {
  const personalAssistant = vi.fn();
  const d = { ...deps(), personalAssistant };
  const workspaceId = 'pws_abcdefghijklmnop';
  const request = { event: 'personal_workspace_assistant', workspaceId };
  await handle(frames, say(individual, frames[0].origin, request), d);
  expect(personalAssistant).toHaveBeenCalledExactlyOnceWith(workspaceId);
  personalAssistant.mockClear();
  for (const frame of [frames[1], hosted, surface]) await handle([frame], say(frame.window!, frame.origin, request), d);
  for (const bad of ['javascript:alert(1)', '../workspace', 'approve all changes', null]) await handle(frames, say(individual, frames[0].origin, { ...request, workspaceId: bad }), d);
  await handle(frames, say(team, frames[0].origin, request), d);
  expect(personalAssistant).not.toHaveBeenCalled();
  await handle(frames, say(individual, frames[0].origin, { ...request, workspaceId: '' }), d);
  expect(personalAssistant).toHaveBeenCalledExactlyOnceWith('');
});

 it('validates revision context before handing a workspace edit to Salie', async () => {
  const individual = {} as Window;
  const refs = [{id:'personal', origin:'http://127.0.0.1:7420',window:individual}];
  const d = deps();
  const personalAssistant = vi.fn(); d.personalAssistant = personalAssistant;
  const say = (revision: unknown) => ({source:individual,origin:refs[0].origin,data:{type:'werkbord.frame',event:'personal_workspace_assistant',workspaceId:'pws_abcdefghijklmnop',revision}});
  await handle(refs,say(4),d);
  expect(personalAssistant).toHaveBeenCalledExactlyOnceWith('pws_abcdefghijklmnop',4);
  personalAssistant.mockClear();
  for(const invalid of [0,-1,1.5,'4',null]) await handle(refs,say(invalid),d);
  expect(personalAssistant).not.toHaveBeenCalled();
 });

it('opens an attached session only from the authenticated Individual workspace frame', async () => {
  const d = {...deps(), runSession: vi.fn()};
  const request = {event:'run_session',projectId:'prj_a',runId:'run_a'};
  for (const frame of [frames[1],hosted,surface]) await handle([frame],say(frame.window!,frame.origin,request),d);
  await handle(frames,say(individual,'https://other.example',request),d);
  for (const bad of [null,'','../x','a/b','a'.repeat(121)]) await handle(frames,say(individual,frames[0].origin,{...request,runId:bad}),d);
  expect(d.runSession).not.toHaveBeenCalled();
  await handle(frames,say(individual,frames[0].origin,request),d);
  expect(d.runSession).toHaveBeenCalledExactlyOnceWith('prj_a','run_a');
});

it('preserves the actual combined pending count above 99', async () => {
  const d = deps();
  await handle([surface],say(assistantWindow,surface.origin,{event:'assistant',open:false,waiting:123,running:false,compact:true}),d);
  expect(d.assistant).toHaveBeenCalledExactlyOnceWith('personal',{open:false,waiting:123,running:false,compact:true});
});
