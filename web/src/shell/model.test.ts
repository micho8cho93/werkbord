import { describe, expect, it, vi } from 'vitest';
import type { App, Overview, WorkspaceView } from './types';

const native = vi.hoisted(() => ({ app: {} as App }));
vi.mock('./native', () => ({ ask: (f: (app: App) => unknown) => f(native.app) }));
import { Model } from './model.svelte';

const individual = { id: 'personal', kind: 'personal' as const, name: 'Universal', state: 'ready' as const, source: 'Werkbord' };
const team = { id: 'team:main', kind: 'team' as const, name: 'Team', state: 'ready' as const, source: 'Team' };
const view = (items: WorkspaceView['items']): WorkspaceView => ({ items, selected: 'personal', problems: [], team: { state: 'ready' }, teamInstaller: true, invitation: false });

describe('workspace retirement', () => {
  it('removes frames and rows on leave and rejects an older overview arriving afterwards', async () => {
    const model = new Model();
    model.view = view([individual, team]);
    model.frames[team.id] = { target: { id: team.id, kind: 'team', url: 'http://127.0.0.1:7431', origin: 'http://127.0.0.1:7431' }, ready: true };
    const old: Overview = { entries: [{ workspace: individual }, { workspace: team }], at: '' };
    model.overview = old;
    let finish!: (overview: Overview) => void;
    native.app.Overview = () => new Promise(resolve => { finish = resolve; });
    native.app.Workspaces = async () => view([individual]);
    const pending = model.refreshOverview();
    await model.refresh();
    expect(model.frames[team.id]).toBeUndefined();
    expect(model.overview?.entries.map(e => e.workspace.id)).toEqual(['personal']);
    finish(old);
    await pending;
    expect(model.overview?.entries.map(e => e.workspace.id)).toEqual(['personal']);
  });
});

describe('Team activation', () => {
  it('says update for an older service, and shows the installer\'s refusal as it is', async () => {
    const model = new Model();
    model.view = { ...view([individual]), team: { state: 'outdated', detail: 'older' } };
    const seen: string[] = [];
    native.app.Workspaces = async () => model.view as WorkspaceView;
    native.app.ActivateTeam = async () => {
      seen.push(model.busy);
      throw new Error('Team\'s service on this Mac belongs to a Team workspace, so it will not be replaced or removed automatically.');
    };
    await model.activateTeam();
    expect(seen).toEqual(['Updating Team…']);
    expect(model.error).toContain('belongs to a Team workspace');
    expect(model.notice).not.toContain('updated');
    expect(model.busy).toBe('');
  });

  it('says set up when nothing is installed', async () => {
    const model = new Model();
    model.view = { ...view([individual]), team: { state: 'not_installed', detail: '' } };
    const seen: string[] = [];
    native.app.Workspaces = async () => model.view as WorkspaceView;
    native.app.ActivateTeam = async () => {
      seen.push(model.busy);
      return model.view!.team;
    };
    await model.activateTeam();
    expect(seen).toEqual(['Setting up Team…']);
    expect(model.notice).toBe('Team is set up.');
  });
});

describe('the assistant’s frame', () => {
  const personalTarget = { id: 'personal', kind: 'personal' as const, url: 'http://127.0.0.1:7420/#token=t0k', origin: 'http://127.0.0.1:7420' };
  const button = { x: 12, y: 40, width: 180, height: 32 };

  it('is made the first time the assistant opens, over the whole window, and asks the frame to open from the button', async () => {
    const model = new Model();
    const loaded: string[] = [];
    native.app.LoadWorkspace = async (id: string) => (loaded.push(id), personalTarget);
    expect(model.assistantFrame).toBeNull();
    await model.toggleAssistant(button);
    expect(loaded).toEqual(['personal']);
    expect(model.assistantFrame).toEqual({ url: 'http://127.0.0.1:7420/?surface=assistant#token=t0k', origin: personalTarget.origin });
    expect(model.assistantShown).toBe(true);
    expect(model.assistantAsk).toEqual({ action: 'open', rect: button });
    // It is not one of the workspaces’ frames, and nothing the person was looking at is changed.
    expect(model.frames).toEqual({});
    expect(model.page).toBe('inbox');
    expect(model.current).toBe('personal');
  });

  it('is kept once made, and is not made again', async () => {
    const model = new Model();
    let n = 0;
    native.app.LoadWorkspace = async () => (n++, personalTarget);
    await model.toggleAssistant(button);
    model.setAssistant({ open: true, waiting: 0, running: false });
    model.setAssistant({ open: false, waiting: 0, running: false });
    await model.toggleAssistant(button);
    expect(n).toBe(1);
  });

  it('is shown while the assistant is open, put away when it has closed, and focus goes back to the button', async () => {
    const model = new Model();
    native.app.LoadWorkspace = async () => personalTarget;
    const focus = vi.fn();
    model.assistantButton = { focus } as unknown as HTMLElement;
    await model.toggleAssistant(button);
    // The frame says it is closed when it starts, before the opening that was asked for begins: it stays on screen for that.
    model.setAssistant({ open: false, waiting: 0, running: false });
    expect(model.assistantShown).toBe(true);
    model.setAssistant({ open: true, waiting: 1, running: true });
    expect(model.assistant).toEqual({ open: true, waiting: 1, running: true });
    expect(model.assistantShown).toBe(true);
    model.setAssistant({ open: false, waiting: 1, running: false });
    expect(model.assistantShown).toBe(false);
    await Promise.resolve();
    expect(focus).toHaveBeenCalled();
  });

  it('opens an editable personal workspace draft in the existing assistant frame', async () => {
    const model = new Model();
    native.app.LoadWorkspace = async () => personalTarget;
    const id = 'pws_abcdefghijklmnop';
    await model.toggleAssistant(button, id);
    expect(model.assistantAsk).toEqual({ action: 'open', rect: button, personalWorkspace: id });
    model.setAssistant({ open: true, waiting: 0, running: false });
    await model.toggleAssistant(button, '');
    expect(model.assistantAsk).toEqual({ action: 'open', rect: button, personalWorkspace: '' });
  });

  it('asks an open assistant to close, from the button, rather than opening it again', async () => {
    const model = new Model();
    native.app.LoadWorkspace = async () => personalTarget;
    await model.toggleAssistant(button);
    model.assistantAsk = null;
    model.setAssistant({ open: true, waiting: 0, running: false });
    await model.toggleAssistant(button);
    expect(model.assistantAsk).toEqual({ action: 'close', rect: button });
  });

  it('is not left over the window if the frame never answers', async () => {
    vi.useFakeTimers();
    try {
      const model = new Model();
      native.app.LoadWorkspace = async () => personalTarget;
      await model.toggleAssistant(button);
      expect(model.assistantShown).toBe(true);
      vi.advanceTimersByTime(5100);
      expect(model.assistantShown).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it('says why when it cannot be made, and shows nothing', async () => {
    const model = new Model();
    native.app.LoadWorkspace = async () => {
      throw new Error('Werkbord is not running on this computer.');
    };
    await model.toggleAssistant(button);
    expect(model.error).toMatch(/not running/);
    expect(model.assistantShown).toBe(false);
    expect(model.assistantFrame).toBeNull();
  });

  it('is closed by going somewhere else in the window, which it covers but the keyboard and the app’s menu can still reach', async () => {
    const model = new Model();
    model.view = view([individual, team]);
    native.app.LoadWorkspace = async () => personalTarget;
    native.app.OpenWorkspace = async () => ({ ...personalTarget, id: team.id, kind: 'team' as const });
    native.app.Workspaces = async () => model.view as WorkspaceView;
    await model.toggleAssistant(button);
    model.setAssistant({ open: true, waiting: 0, running: false });
    model.assistantAsk = null;
    model.go('inbox');
    expect(model.assistantAsk).toMatchObject({ action: 'close' });
  });
});


describe('startup', () => {
  it('opens Universal My Work even when the saved workspace is Team, without selecting a workspace', async () => {
    const model = new Model();
    const open = vi.fn();
    native.app.OpenWorkspace = open;
    native.app.LoadWorkspace = vi.fn().mockResolvedValue({ id: 'personal', kind: 'personal', url: 'http://127.0.0.1:7420/#token=t0k', origin: 'http://127.0.0.1:7420' });
    native.app.Workspaces = async () => ({ ...view([individual, team]), selected: team.id });
    native.app.Overview = async () => ({ entries: [], at: '' });
    try {
      await model.start();
      expect(model.page).toBe('inbox');
      expect(model.universal).toBe(true);
      expect(open).not.toHaveBeenCalled();
      expect(Object.keys(model.frames)).toEqual(['personal']); // Background readiness check, without selecting it.
      await model.refresh();
      expect(model.page).toBe('inbox');
    } finally { model.stop(); }
  });
});

import { todoWorkspace } from '../lib/personal/example';
import type { Workspace } from '../lib/personal/schema';
describe('generated workspace navigation', () => {
  it('retains context, routes, pending navigation and both underlying frames', async () => {
    const model = new Model();
    model.view = view([individual, team]);
    const target = {id: 'personal', kind: 'personal' as const, url: 'http://127.0.0.1:7420', origin: 'http://127.0.0.1:7420'};
    model.frames = {personal: {target, ready: true}, [team.id]: {target: {...target, id: team.id, kind: 'team'}, ready: true}};
    model.places = {personal: '#/p/one/board', [team.id]: '?tab=reviews'};
    const workspace = {...todoWorkspace('pcl_abcdefghijklmnop'), id: 'pws_abcdefghijklmnop', revision: 3, createdAt: '', updatedAt: '', visibility: {universal: true, individual: true, teamWorkspaceIds: [team.id]}} as Workspace;
    model.generated = [workspace];
    const before = JSON.stringify(model.frames);
    model.universal = false; model.current = team.id;
    model.openGenerated(workspace.id);
    expect(model.page).toBe('generated');
    expect(model.current).toBe(team.id);
    expect(model.shownWorkspace).toBeUndefined();
    expect(JSON.stringify(model.frames)).toBe(before);
    expect(model.places).toEqual({personal: '#/p/one/board', [team.id]: '?tab=reviews'});
    model.go('workspace');
    expect(model.shownWorkspace).toBe(team.id);
    model.universal = true; model.openGenerated(workspace.id);
    expect(model.universal).toBe(true);
    expect(model.generatedID).toBe(workspace.id);
    native.app.LoadWorkspace = async () => target;
    await model.customizeGenerated(workspace.id, workspace.revision);
    expect(model.assistantAsk).toMatchObject({personalWorkspace: workspace.id, personalRevision: 3});
  });
});


it('reopens a hidden saved workspace from the library without creating a display location', () => {
  const model = new Model();
  const saved = {id: 'pws_abcdefghijklmnop', revision: 1, name: 'Hidden planner', icon: 'calendar' as const, createdAt: '2026-10-23T12:00:00Z', updatedAt: '2026-10-23T12:00:00Z', uiSchemaVersion: 1 as const, collectionIds: [], visibility: {universal:false, individual:false, teamWorkspaceIds:[]}, definition: {views:[], layout:{type:'heading' as const,text:'Hidden'}}};
  model.generated = [saved];
  model.openGenerated(saved.id);
  expect(model.page).toBe('inbox');
  model.openGenerated(saved.id, true);
  expect(model.page).toBe('generated');
  expect(model.generatedID).toBe(saved.id);
  expect(model.visibleGenerated).toEqual([]);
  expect(model.generated[0].visibility).toEqual(saved.visibility);
});

describe('one sidebar, no switcher', () => {
  const target = (id: string) => ({ id, kind: id === 'personal' ? 'personal' as const : 'team' as const, url: 'http://127.0.0.1:1/', origin: 'http://127.0.0.1:1' });

  it('goes to a place in the workspace that is open without asking the app again, and opens any other', async () => {
    const model = new Model();
    model.view = view([individual, team]);
    model.frames.personal = { target: target('personal'), ready: true };
    model.current = 'personal';
    const opened: string[] = [];
    native.app.OpenWorkspace = async (id: string) => { opened.push(id); return target(id); };
    native.app.Workspaces = async () => view([individual, team]);
    await model.goTo('personal', '#/p/prj_1');
    expect(opened).toEqual([]);
    expect(model.frames.personal.pending).toBe('#/p/prj_1');
    expect(model.page).toBe('workspace');
    expect(model.universal).toBe(false);
    await model.goTo('team:main', '?tab=members');
    expect(opened).toEqual(['team:main']);
    expect(model.current).toBe('team:main');
    expect(model.frames['team:main'].pending).toBe('?tab=members');
  });

  it('folds a section and remembers it, and unfolds it again', () => {
    const kept = new Map<string, string>();
    vi.stubGlobal('localStorage', { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) });
    try {
      const model = new Model();
      model.toggleSection('team:main');
      expect(model.folded).toEqual(['team:main']);
      expect(JSON.parse(kept.get('werkbord.shell.sections') ?? '[]')).toEqual(['team:main']);
      expect(new Model().folded).toEqual(['team:main']);
      model.toggleSection('team:main');
      expect(model.folded).toEqual([]);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('opens and closes the go-to palette, and shows Salie’s saved workspaces in the section they were made for', () => {
    const model = new Model();
    model.togglePalette();
    expect(model.paletteOpen).toBe(true);
    model.closePalette();
    expect(model.paletteOpen).toBe(false);
    const saved = (id: string, visibility: { universal: boolean; individual: boolean; teamWorkspaceIds?: string[] }) => ({ id, name: id, icon: 'auto', visibility }) as never;
    model.generated = [saved('pws_a', { universal: true, individual: false }), saved('pws_b', { universal: false, individual: true }), saved('pws_c', { universal: false, individual: false, teamWorkspaceIds: ['team:main'] })];
    expect(model.generatedFor('universal').map((w) => w.id)).toEqual(['pws_a']);
    expect(model.generatedFor('personal').map((w) => w.id)).toEqual(['pws_b']);
    expect(model.generatedFor('team:main').map((w) => w.id)).toEqual(['pws_c']);
  });
});

describe('the one create-and-join flow', () => {
  const setupTarget = { id: 'team:ws_3', kind: 'team' as const, name: 'Smith & Co', state: 'ready' as const, source: 'Team' };
  const store: Record<string, string> = {};
  const fresh = (items: WorkspaceView['items'] = [individual]) => {
  vi.stubGlobal('localStorage', { getItem: (k: string) => store[k] ?? null, setItem: (k: string, v: string) => { store[k] = v; }, removeItem: (k: string) => { delete store[k]; } });
    const model = new Model();
    model.view = view(items);
    native.app.Workspaces = async () => model.view as WorkspaceView;
    native.app.Overview = async () => ({ entries: [], at: '' });
    native.app.OpenWorkspace = async (id: string) => ({ id, kind: 'team', url: 'http://127.0.0.1:7431', origin: 'http://127.0.0.1:7431' });
    delete store['werkbord.shell.runner-wanted'];
    return model;
  };

  it('creates, waits for the service, connects the runner once when asked, and opens the team', async () => {
    const model = fresh();
    const calls: string[] = [];
    native.app.CreateTeam = async (name: string, owner: string) => { calls.push(`create ${name}/${owner}`); return { id: 'team:ws_3' }; };
    let polls = 0;
    native.app.TeamSetupState = async () => ({ working: polls++ ? '' : 'Creating your workspace', done: polls > 1, waiting: false, name: 'Smith & Co' });
    native.app.Relay = async (id: string, method: string) => { calls.push(`${method} ${id}`); return null; };
    await model.createTeam('Smith & Co', 'Sam', true);
    expect(model.setup?.id).toBe('team:ws_3');
    model.view = view([individual, setupTarget]);
    await vi.waitFor(() => expect(model.setup).toBeNull(), { timeout: 5000 });
    expect(calls).toEqual(['create Smith & Co/Sam', 'ConnectRunner team:ws_3']);
    expect(model.current).toBe('team:ws_3');
    expect(model.addOpen).toBe(false);
    expect(model.notice).toContain('Smith & Co is ready');
    expect(model.notice).toContain('runner is connected');
  });

  it('does not connect a runner the person did not ask for', async () => {
    const model = fresh();
    const calls: string[] = [];
    native.app.CreateTeam = async () => ({ id: 'team:ws_3' });
    native.app.TeamSetupState = async () => ({ working: '', done: true, waiting: false, name: 'Acme' });
    native.app.Relay = async (_id: string, method: string) => { calls.push(method); return null; };
    await model.createTeam('Acme', 'Sam', false);
    await vi.waitFor(() => expect(model.setup).toBeNull());
    expect(calls).toEqual([]);
  });

  it('shows the app’s reason when it cannot start, and leaves nothing running', async () => {
    const model = fresh();
    native.app.CreateTeam = async () => { throw new Error('this computer already hosts another Team workspace'); };
    await model.createTeam('Acme', 'Sam', true);
    expect(model.error).toContain('already hosts');
    expect(model.setup).toBeNull();
    expect(model.busy).toBe('');
  });

  it('asks the app to set Team up first, and stops if the person says no', async () => {
    const model = fresh();
    model.view = { ...view([individual]), team: { state: 'not_installed', detail: '' } };
    let created = 0;
    native.app.ActivateTeam = async () => { throw new Error('Team was not set up'); };
    native.app.CreateTeam = async () => { created++; return { id: 'team:ws_3' }; };
    await model.createTeam('Acme', 'Sam', true);
    expect(created).toBe(0);
    expect(model.setup).toBeNull();
  });

  it('waits for an administrator without blocking, and finishes and connects the runner once the person is let in', async () => {
    const model = fresh();
    model.waitingPause = 5;
    const calls: string[] = [];
    let approved = false;
    native.app.JoinTeam = async () => ({ id: 'team:ws_3' });
    native.app.TeamSetupState = async () => approved ? { working: '', done: true, waiting: false, name: 'Acme' } : { working: '', done: false, waiting: true };
    native.app.Relay = async (id: string, method: string) => { calls.push(`${method} ${id}`); return null; };
    await model.joinTeam('werkbord://join/abc', 'Sam', 'Office Mac', true, 'Acme');
    await vi.waitFor(() => expect(model.setup?.waiting).toBe(true));
    expect(JSON.parse(store['werkbord.shell.runner-wanted'])).toEqual(['team:ws_3']);
    // The sidebar lists a join that waits as connecting. Nothing is asked of the runner, and the dialog does not finish.
    model.view = view([individual, { ...setupTarget, state: 'connecting' }]);
    await model.refresh();
    await new Promise((r) => setTimeout(r, 30));
    expect(calls).toEqual([]);
    expect(model.setup?.waiting).toBe(true);
    // Approved: the dialog finishes, the runner is asked about once, and the wish is spent.
    approved = true;
    model.view = view([individual, setupTarget]);
    await vi.waitFor(() => expect(model.setup).toBeNull());
    expect(calls).toEqual(['ConnectRunner team:ws_3']);
    expect(store['werkbord.shell.runner-wanted']).toBeUndefined();
  });

  it('asks about the runner once for a join that was waiting when the window was closed and reopened', async () => {
    const model = fresh();
    const calls: string[] = [];
    store['werkbord.shell.runner-wanted'] = JSON.stringify(['team:ws_3', 'team:gone']);
    native.app.Relay = async (id: string, method: string) => { calls.push(`${method} ${id}`); return null; };
    model.view = view([individual, { ...setupTarget, state: 'connecting' }]);
    await model.refresh();
    expect(calls).toEqual([]);
    expect(JSON.parse(store['werkbord.shell.runner-wanted'])).toEqual(['team:ws_3']);
    model.view = view([individual, setupTarget]);
    await model.refresh();
    await model.refresh();
    expect(calls).toEqual(['ConnectRunner team:ws_3']);
    expect(store['werkbord.shell.runner-wanted']).toBeUndefined();
  });

  it('withdraws a waiting join and forgets the wish', async () => {
    const model = fresh();
    const calls: string[] = [];
    native.app.JoinTeam = async () => ({ id: 'team:ws_3' });
    native.app.TeamSetupState = async () => ({ working: '', done: false, waiting: true });
    model.waitingPause = 5;
    native.app.CancelTeamSetup = async (id: string) => { calls.push(id); };
    await model.joinTeam('werkbord://join/abc', 'Sam', 'Mac', true);
    await vi.waitFor(() => expect(model.setup?.waiting).toBe(true));
    await model.cancelSetup();
    expect(calls).toEqual(['team:ws_3']);
    expect(model.setup).toBeNull();
    expect(store['werkbord.shell.runner-wanted']).toBeUndefined();
  });

  it('shows a failure from the service and lets the person start over', async () => {
    const model = fresh();
    native.app.JoinTeam = async () => ({ id: 'team:ws_3' });
    native.app.TeamSetupState = async () => ({ working: '', done: false, waiting: false, error: 'the workspace did not accept this invitation' });
    await model.joinTeam('werkbord://join/abc', 'Sam', 'Mac', false);
    await vi.waitFor(() => expect(model.setup?.error).toContain('did not accept'));
  });

  it('leaves a team that exists but could not connect alone, and closes the dialog', async () => {
    const model = fresh();
    const cancelled: string[] = [];
    native.app.JoinTeam = async () => ({ id: 'team:ws_3' });
    native.app.TeamSetupState = async () => ({ working: '', done: false, waiting: false, exists: true, error: 'its private network overlaps another team’s' });
    native.app.CancelTeamSetup = async (id: string) => { cancelled.push(id); };
    model.addOpen = true;
    await model.joinTeam('werkbord://join/abc', 'Sam', 'Mac', true);
    await vi.waitFor(() => expect(model.setup?.exists).toBe(true));
    await model.cancelSetup();
    expect(cancelled).toEqual([]);
    expect(model.setup).toBeNull();
    expect(model.addOpen).toBe(false);
  });
});
