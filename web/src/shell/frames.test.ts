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
  it('opens map sharing only from an authenticated main Team frame with a bounded project ID', async () => {
    const productMap = vi.fn();
    const d = { ...deps(), productMap };
    const message = { event: 'product_map', projectId: 'project-123' };
    for (const frame of [frames[0], hosted, surface, { ...frames[1], hosted: true }]) {
      await handle([frame], say(frame.window!, frame.origin, message), d);
    }
    await handle(frames, say(team, 'https://other.example', message), d);
    for (const projectId of ['', '../escape', 'x'.repeat(121), null]) {
      await handle(frames, say(team, frames[1].origin, { ...message, projectId }), d);
    }
    expect(productMap).not.toHaveBeenCalled();
    await handle(frames, say(team, frames[1].origin, message), d);
    expect(productMap).toHaveBeenCalledExactlyOnceWith('team:main', 'project-123');
  });
  it('lets only the main page of the person\'s own Werkbord ask to be signed in again, and answers with why it could not', async () => {
    const reauth = vi.fn().mockResolvedValueOnce(null).mockResolvedValueOnce('It could not read the token.');
    const d = { ...deps(), reauth };
    // Not a Team page, not the assistant's frame, not from another origin.
    await handle(frames, say(team, frames[1].origin, { event: 'reauth' }), d);
    await handle([surface], say(assistantWindow, surface.origin, { event: 'reauth' }), d);
    await handle(frames, say(individual, 'https://other.example', { event: 'reauth' }), d);
    expect(reauth).not.toHaveBeenCalled();
    // Signed in again: the page is replaced, so there is nothing to answer.
    await handle(frames, say(individual, frames[0].origin, { event: 'reauth' }), d);
    expect(reauth).toHaveBeenLastCalledWith('personal', false);
    expect(d.reply).not.toHaveBeenCalled();
    await handle(frames, say(individual, frames[0].origin, { event: 'reauth', manual: true }), d);
    expect(reauth).toHaveBeenLastCalledWith('personal', true);
    expect(d.reply).toHaveBeenLastCalledWith(frames[0], { type: 'werkbord.reauth', ok: false, message: 'It could not read the token.' });
  });
  it('may say which theme the person chose, and nothing else about it', async () => {
    const d = deps();
    for (const theme of ['dark', 'light', '']) await handle(frames, say(individual, frames[0].origin, { event: 'theme', theme }), d);
    for (const theme of ['blue', 1, null, undefined]) await handle(frames, say(individual, frames[0].origin, { event: 'theme', theme }), d);
    expect(vi.mocked(d.theme).mock.calls).toEqual([['personal', 'dark'], ['personal', 'light'], ['personal', '']]);
  });
  it('may say which style the person chose, and nothing else about it', async () => {
    const d = { ...deps(), style: vi.fn() };
    for (const style of ['paper', 'graphite', 'classic', 'cloud']) await handle(frames, say(individual, frames[0].origin, { event: 'style', style }), d);
    for (const style of ['', 'neon', 1, null, undefined]) await handle(frames, say(individual, frames[0].origin, { event: 'style', style }), d);
    expect(vi.mocked(d.style).mock.calls).toEqual([['personal', 'paper'], ['personal', 'graphite'], ['personal', 'classic'], ['personal', 'cloud']]);
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
  it('lets only a Team’s own page send the person to Plan, and carries nothing with it', async () => {
    const d = { ...deps(), plan: vi.fn() };
    // Not Individual, not a page shown for Settings or Help, not the assistant, and not from another origin.
    await handle(frames, say(individual, frames[0].origin, { event: 'plan' }), d);
    await handle([{ ...frames[1], hosted: true }], say(team, frames[1].origin, { event: 'plan' }), d);
    await handle([surface], say(assistantWindow, surface.origin, { event: 'plan' }), d);
    await handle(frames, say(team, 'https://evil.example', { event: 'plan' }), d);
    expect(d.plan).not.toHaveBeenCalled();
    await handle(frames, say(team, frames[1].origin, { event: 'plan', place: '?tab=x', key: 'wbpremium-x' }), d);
    expect(d.plan).toHaveBeenCalledExactlyOnceWith('team:main');
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
    await handle([surface], { source: assistantWindow, origin: surface.origin, data: { type: 'werkbord.frame', event: 'open', target: 'personal', place: '#/p/prj_1/git/branch/local/werkbord%2Ffix' } }, d);
    expect(d.open).toHaveBeenCalledExactlyOnceWith('personal', 'personal', '#/p/prj_1/git/branch/local/werkbord%2Ffix');
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

it('opens a coding-agent session in Agents only by a validated session key, from Individual or Salie and no other frame', async () => {
  const d = {...deps(), agentSession: vi.fn()};
  const request = {event:'agent_session',key:'run:run_a'};
  for (const frame of [frames[1],hosted]) await handle([frame],say(frame.window!,frame.origin,request),d);
  await handle(frames,say(individual,'https://other.example',request),d);
  for (const bad of [null,'','run','run:','task:tsk_1','run:../x','run:a/b','terminal:a b','run:'+'a'.repeat(97),{}]) await handle(frames,say(individual,frames[0].origin,{...request,key:bad}),d);
  expect(d.agentSession).not.toHaveBeenCalled();
  await handle(frames,say(individual,frames[0].origin,request),d);
  expect(d.agentSession).toHaveBeenLastCalledWith('run:run_a');
  // Salie's own frame may ask too: a card in a conversation leads to the session it started.
  await handle([surface],say(assistantWindow,surface.origin,{event:'agent_session',key:'terminal:abc-123'}),d);
  expect(d.agentSession).toHaveBeenLastCalledWith('terminal:abc-123');
  // Agents without a key is the list.
  await handle(frames,say(individual,frames[0].origin,{event:'agent_session'}),d);
  expect(d.agentSession).toHaveBeenLastCalledWith(undefined);
  expect(d.agentSession).toHaveBeenCalledTimes(3);
});

it('lets only the main Individual frame go back to Salie, naming at most a conversation ID', async () => {
  const d = {...deps(), openSalie: vi.fn()};
  const request = {event:'salie',conversation:'ast_k3j9x2m4q7p1a8z5'};
  for (const frame of [frames[1],hosted,surface]) await handle([frame],say(frame.window!,frame.origin,request),d);
  await handle(frames,say(individual,'https://other.example',request),d);
  for (const bad of ['','a b','../x','a'.repeat(81),5,{}]) await handle(frames,say(individual,frames[0].origin,{...request,conversation:bad}),d);
  expect(d.openSalie).not.toHaveBeenCalled();
  await handle(frames,say(individual,frames[0].origin,request),d);
  expect(d.openSalie).toHaveBeenLastCalledWith('ast_k3j9x2m4q7p1a8z5');
  await handle(frames,say(individual,frames[0].origin,{event:'salie'}),d);
  expect(d.openSalie).toHaveBeenLastCalledWith(undefined);
});

it('preserves the actual combined pending count above 99', async () => {
  const d = deps();
  await handle([surface],say(assistantWindow,surface.origin,{event:'assistant',open:false,waiting:123,running:false}),d);
  expect(d.assistant).toHaveBeenCalledExactlyOnceWith('personal',{open:false,waiting:123,running:false});
});

it('takes where a session was opened from only as a chain of known origins, and only for a session or the window\'s own pages', async () => {
  const d = {...deps(), agentSession: vi.fn(), page: vi.fn()};
  const from = 'task:prj_a:tsk_1>calendar';
  for (const bad of ['', 'nonsense', 'task:../x:y', 'work>work>work>work', 5, {}, 'javascript:alert(1)']) await handle(frames,say(individual,frames[0].origin,{event:'agent_session',key:'run:run_a',from:bad}),d);
  expect(d.agentSession).not.toHaveBeenCalled();
  await handle(frames,say(individual,frames[0].origin,{event:'agent_session',key:'run:run_a',from}),d);
  expect(d.agentSession).toHaveBeenLastCalledWith('run:run_a', from);
  await handle([surface],say(assistantWindow,surface.origin,{event:'agent_session',key:'run:run_a',from:'salie:ast_k3j9x2m4q7p1a8z5'}),d);
  expect(d.agentSession).toHaveBeenLastCalledWith('run:run_a', 'salie:ast_k3j9x2m4q7p1a8z5');
  // Back to Work or Calendar: from the main frame of the person's own Werkbord, to those two pages only.
  for (const frame of [frames[1],hosted,surface]) await handle([frame],say(frame.window!,frame.origin,{event:'page',page:'inbox'}),d);
  for (const page of ['settings','agents','workspace','','../x',5,undefined]) await handle(frames,say(individual,frames[0].origin,{event:'page',page}),d);
  await handle(frames,say(individual,'https://other.example',{event:'page',page:'inbox'}),d);
  expect(d.page).not.toHaveBeenCalled();
  await handle(frames,say(individual,frames[0].origin,{event:'page',page:'inbox'}),d);
  await handle(frames,say(individual,frames[0].origin,{event:'page',page:'calendar'}),d);
  expect(d.page.mock.calls).toEqual([['inbox'],['calendar']]);
});
