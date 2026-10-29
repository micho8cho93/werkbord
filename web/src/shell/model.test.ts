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

  it('signs a refused page in again at the place it was, and does not do it in a circle', async () => {
    const model = new Model();
    model.view = view([individual]);
    model.frames.personal = { target: target('personal'), ready: true };
    model.places.personal = '#/p/prj_1';
    let url = 'http://127.0.0.1:1/#token=NEW';
    native.app.LoadWorkspace = async (id: string) => ({ ...target(id), url });
    expect(await model.reauth('personal')).toBeNull();
    expect(model.frames.personal.target.url).toBe(url);
    expect(model.frames.personal.epoch).toBe(1);
    expect(model.frames.personal.ready).toBe(false);
    expect(model.frames.personal.pending).toBe('#/p/prj_1');
    // Refused again straight away: it says so rather than loading the page in a circle.
    expect(await model.reauth('personal')).toMatch(/did not help/);
    expect(model.frames.personal.epoch).toBe(1);
    // The person asking is always attempted, and an app that cannot read the token says why.
    native.app.LoadWorkspace = async () => { throw new Error('this app could not read Werkbord\'s access token'); };
    expect(await model.reauth('personal', true)).toMatch(/could not read/);
    expect(model.frames.personal.epoch).toBe(1);
    url = 'http://127.0.0.1:1/#token=NEWER';
    native.app.LoadWorkspace = async (id: string) => ({ ...target(id), url });
    expect(await model.reauth('personal', true)).toBeNull();
    expect(model.frames.personal.epoch).toBe(2);
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


it('keeps Salie unavailable on a remote Personal controller', async () => {
  const model = new Model();
  model.view = view([{...individual, deviceRoles: ['control_client', 'runner']}, team]);
  expect(model.remotePersonal).toBe(true);
  const settings = model.settingsGroups.flatMap(g => g.sections.map(s => s.id));
  expect(settings).not.toContain('personal/defaults');
  expect(settings).toContain('personal/runners');
  await model.prepareAssistant();
  await model.toggleAssistant({x:0,y:0,width:0,height:0});
  expect(model.assistantFrame).toBeNull();
});

it('opens and closes the go-to palette', () => {
  const model = new Model();
  model.togglePalette();
  expect(model.paletteOpen).toBe(true);
  model.closePalette();
  expect(model.paletteOpen).toBe(false);
});
