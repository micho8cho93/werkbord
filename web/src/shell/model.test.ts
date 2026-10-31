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
    model.assistantSidebarButton = { focus } as unknown as HTMLElement;
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

  it('remembers a composite project selection and the virtual All Projects choice', () => {
    const kept = new Map<string, string>();
    vi.stubGlobal('localStorage', { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) });
    try {
      const model = new Model();
      const selection = { workspace: 'team:main', project: 'p' };
      model.selectProject(selection);
      expect(JSON.parse(kept.get('werkbord.shell.project')!)).toEqual(['team:main', 'p']);
      expect(new Model().selectedProject).toEqual(selection);
      model.selectProject(null);
      expect(new Model().selectedProject).toBeNull();
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

describe('unified project selection', () => {
  it('persists selection across destinations, settings and an assistant opening', async () => {
    const model = new Model();
    model.view = view([individual, team]);
    native.app.Overview = async () => ({ at: '', entries: [] });
    native.app.LoadWorkspace = async () => ({ id: 'personal', kind: 'personal', url: 'http://localhost/', origin: 'http://localhost' });
    native.app.OpenWorkspace = async () => ({ id: 'personal', kind: 'personal', url: 'http://localhost/', origin: 'http://localhost' });
    native.app.Workspaces = async () => view([individual, team]);
    model.selectProject({ workspace: team.id, project: 'same-id' });
    for (const destination of ['inbox', 'calendar', 'agents', 'analytics'] as const) {
      model.go(destination);
      expect(model.selectedProject).toEqual({ workspace: team.id, project: 'same-id' });
    }
    model.openSettings('general/appearance');
    expect(model.selectedProject?.workspace).toBe(team.id);
    await model.toggleAssistant({ x: 0, y: 0, width: 0, height: 0 });
    expect(model.selectedProject?.project).toBe('same-id');
    model.setAssistant({ open: true, waiting: 0, running: false });
  });
  it('keeps an offline selection but clears it when workspace membership disappears', async () => {
    const model = new Model();
    model.selectedProject = { workspace: team.id, project: 'p' };
    native.app.Workspaces = async () => view([individual, { ...team, state: 'offline' }]);
    await model.refresh();
    expect(model.selectedProject?.project).toBe('p');
    native.app.Workspaces = async () => view([individual]);
    await model.refresh();
    expect(model.selectedProject).toBeNull();
  });
  it('drops a project removed by a fresh permission-filtered summary', async () => {
    const model = new Model();
    model.view = view([individual, team]);
    model.selectedProject = { workspace: team.id, project: 'revoked' };
    native.app.Overview = async () => ({ at: '', entries: [{ workspace: team, summary: { schema: '', workspace: team, projects: [], work: [], attention: [], schedule: [], at: '' } }] });
    await model.refreshOverview();
    expect(model.selectedProject).toBeNull();
    expect(model.notice).toContain('no longer accessible');
  });
});

it('marks aggregate reads unavailable when the summary bridge fails', async () => {
  const model = new Model();
  model.view = view([individual]);
  model.overview = { at: '', entries: [{ workspace: individual, summary: { schema: '', workspace: individual, projects: [], work: [], attention: [], schedule: [], at: '' } }] };
  native.app.Overview = async () => { throw new Error('disconnected'); };
  expect(await model.refreshOverview()).toBe(false);
  expect(model.contextOverview?.entries[0].summary).toBeUndefined();
  expect(model.contextOverview?.entries[0].error).toContain('could not be refreshed');
  const firstLoad = new Model();
  firstLoad.view = view([individual]);
  await firstLoad.refreshOverview();
  expect(firstLoad.contextOverview?.entries[0].error).toContain('could not be refreshed');
});

describe('Agents', () => {
  const target = { id: 'personal', kind: 'personal' as const, url: 'http://localhost/', origin: 'http://localhost' };
  const ready = async () => {
    const model = new Model();
    model.view = view([individual, team]);
    native.app.Overview = async () => ({ at: '', entries: [] });
    native.app.Workspaces = async () => view([individual, team]);
    native.app.OpenWorkspace = async () => target;
    return model;
  };
  it('shows the person’s own sessions in their frame, at the session asked for', async () => {
    const model = await ready();
    await model.openAgents('run:run_a');
    expect(model.page).toBe('agents');
    expect(model.agentsInFrame).toBe(true);
    expect(model.shownWorkspace).toBe('personal');
    // The model starts on Work, so the session remembers that page for Back.
    expect(model.places.personal).toBe('#/sessions/run%3Arun_a?from=work');
    expect(model.frames.personal.pending).toBe('#/sessions/run%3Arun_a?from=work');
  });
  it('comes back to the session that was open, and to the list when none was', async () => {
    const model = await ready();
    await model.openAgents();
    expect(model.places.personal).toBe('#/sessions');
    await model.openAgents('terminal:t1');
    model.go('inbox');
    model.go('agents');
    await vi.waitFor(() => expect(model.page).toBe('agents'));
    expect(model.places.personal).toBe('#/sessions/terminal%3At1');
  });
  it('is Work again when the Individual page leaves Agents, and Agents again when it returns', async () => {
    const model = await ready();
    await model.openAgents('run:run_a');
    model.frames.personal.pending = undefined;
    model.setPlace('personal', '#/p/prj_a/task/tsk_1');
    expect(model.page).toBe('workspace');
    model.setPlace('personal', '#/sessions/run%3Arun_a');
    expect(model.page).toBe('agents');
  });
  it('opening Salie, or leaving for Salie, closes nothing that is running', async () => {
    const model = await ready();
    native.app.LoadWorkspace = async () => target;
    await model.openAgents('run:run_a');
    await model.openSalie('ast_k3j9x2m4q7p1a8z5');
    expect(model.assistantShown).toBe(true);
    expect(model.assistantAsk).toMatchObject({ action: 'open', conversation: 'ast_k3j9x2m4q7p1a8z5' });
    expect(model.frames.personal).toBeDefined();
  });
  it('falls back to what workspaces report when the person’s own Werkbord cannot be shown', async () => {
    const model = new Model();
    model.view = view([{ ...individual, state: 'unavailable' }, team]);
    native.app.Overview = async () => ({ at: '', entries: [] });
    await model.openAgents('run:run_a');
    expect(model.page).toBe('agents');
    expect(model.agentsInFrame).toBe(false);
  });
});

describe('Back and where a page was opened from', () => {
  const target = { id: 'personal', kind: 'personal' as const, url: 'http://localhost/', origin: 'http://localhost' };
  const summary = { schema: '', workspace: individual, projects: [{ id: 'prj_a', name: 'A', href: '#/p/prj_a/work' }, { id: 'prj_b', name: 'B', href: '#/p/prj_b/work' }], work: [], attention: [], schedule: [], at: '' };
  const ready = () => {
    const model = new Model();
    model.view = view([individual, team]);
    model.overview = { at: '', entries: [{ workspace: individual, summary }, { workspace: team }] };
    native.app.Overview = async () => model.overview as Overview;
    native.app.Workspaces = async () => view([individual, team]);
    native.app.OpenWorkspace = async (id: string) => (id === 'personal' ? target : { id, kind: 'team', url: 'http://127.0.0.1:7431', origin: 'http://127.0.0.1:7431' });
    return model;
  };
  const chain = (place: string | undefined) => new URLSearchParams((place ?? '').split('?')[1] ?? '').get('from');

  it('a task or plan opened from Work, Calendar or Agents remembers that page in its address', async () => {
    for (const [page, from] of [['inbox', 'work'], ['calendar', 'calendar'], ['agents', 'agents']] as const) {
      const model = ready();
      model.page = page;
      await model.goTo('personal', '#/p/prj_a/task/tsk_1');
      expect(chain(model.places.personal), page).toBe(from);
      expect(model.places.personal.startsWith('#/p/prj_a/task/tsk_1?from=')).toBe(true);
    }
    const model = ready();
    model.page = 'inbox';
    await model.goTo('personal', '#/p/prj_a/work?plan=pln_1');
    expect(chain(model.places.personal)).toBe('work');
  });
  it('leaves every other place as it was: a project, a place with a route already, and anything opened from a workspace page', async () => {
    const model = ready();
    model.page = 'inbox';
    await model.goTo('personal', '#/p/prj_a/work');
    expect(model.places.personal).toBe('#/p/prj_a/work');
    await model.goTo('personal', '#/p/prj_a/task/tsk_1?from=salie');
    expect(chain(model.places.personal)).toBe('salie');
    const other = ready();
    other.page = 'workspace';
    await other.goTo('personal', '#/p/prj_a/task/tsk_1');
    expect(other.places.personal).toBe('#/p/prj_a/task/tsk_1');
    expect(other.departed).toBeNull();
  });
  it('Back to Work puts the project context back, even though opening the task moved it to the task’s project', async () => {
    const model = ready();
    model.page = 'inbox';
    model.selectedProject = null;
    await model.goTo('personal', '#/p/prj_a/task/tsk_1');
    expect(model.selectedProject).toEqual({ workspace: 'personal', project: 'prj_a' });
    model.returnTo('inbox');
    expect(model.selectedProject).toBeNull();
    expect(model.page).toBe('inbox');
    expect(model.departed).toBeNull();
    // A context chosen on purpose is kept: Back restores what it left, nothing more.
    model.selectedProject = { workspace: 'personal', project: 'prj_b' };
    await model.goTo('personal', '#/p/prj_b/task/tsk_2');
    model.returnTo('inbox');
    expect(model.selectedProject).toEqual({ workspace: 'personal', project: 'prj_b' });
  });
  it('does not restore a context for a page the person did not leave, and forgets the departure when they go somewhere else', async () => {
    const model = ready();
    model.page = 'inbox';
    await model.goTo('personal', '#/p/prj_a/task/tsk_1');
    model.returnTo('calendar');
    expect(model.selectedProject).toEqual({ workspace: 'personal', project: 'prj_a' });
    await model.goTo('personal', '#/p/prj_a/task/tsk_1');
    model.page = 'inbox';
    model.go('calendar');
    expect(model.departed).toBeNull();
  });
  it('a Team page opened from Work is left as it was and the window keeps the way back', async () => {
    const model = ready();
    model.page = 'inbox';
    await model.goTo('team:main', '?tab=work&project=p&ticket=t');
    expect(model.places['team:main']).toBe('?tab=work&project=p&ticket=t');
    expect(model.departed).toMatchObject({ page: 'inbox', workspace: 'team:main' });
    model.returnTo('inbox');
    expect(model.page).toBe('inbox');
  });
  it('forgets the way back to a window page once the person is moving about inside workspaces', async () => {
    const model = ready();
    model.page = 'inbox';
    await model.goTo('team:main', '?tab=work&project=p&ticket=t');
    expect(model.departed).not.toBeNull();
    await model.goTo('team:main', '?tab=work&project=p&ticket=u');
    expect(model.departed).toBeNull();
    model.page = 'inbox';
    await model.goTo('team:main', '?tab=work&project=p&ticket=t');
    await model.open('personal', '#/p/prj_a/work');
    expect(model.departed).toBeNull();
  });
  it('a session opened from Work remembers it; from Agents, or with a route given, it uses that instead', async () => {
    const model = ready();
    model.page = 'inbox';
    await model.openAgents('run:run_a');
    expect(chain(model.places.personal)).toBe('work');
    await model.openAgents('run:run_b');
    expect(chain(model.places.personal)).toBeNull();
    await model.openAgents('run:run_c', [{ kind: 'task', projectId: 'prj_a', taskId: 'tsk_1' }, { kind: 'calendar' }]);
    expect(chain(model.places.personal)).toBe('task:prj_a:tsk_1>calendar');
  });
  it('lists the sessions of the window’s project context and follows it, an open session staying open', async () => {
    const model = ready();
    model.selectedProject = { workspace: 'personal', project: 'prj_a' };
    await model.openAgents('run:run_a', [{ kind: 'salie', conversation: 'ast_k3j9x2m4q7p1a8z5' }]);
    expect(model.places.personal).toBe('#/sessions/run%3Arun_a?project=prj_a&from=salie%3Aast_k3j9x2m4q7p1a8z5');
    model.selectProject({ workspace: 'personal', project: 'prj_b' });
    await vi.waitFor(() => expect(model.places.personal).toContain('project=prj_b'));
    expect(model.places.personal).toContain('/run%3Arun_a');
    expect(model.places.personal).toContain('from=salie');
    model.selectProject({ workspace: 'team:main', project: 'p' });
    await vi.waitFor(() => expect(model.places.personal).not.toContain('project='));
    expect(model.page).toBe('agents');
    model.selectProject(null);
    await model.openAgents();
    expect(model.places.personal).toBe('#/sessions/run%3Arun_a?from=salie%3Aast_k3j9x2m4q7p1a8z5');
  });
  it('going back to a page of the window never starts, stops or reloads anything', async () => {
    const model = ready();
    const asked: string[] = [];
    native.app.OpenWorkspace = async (id: string) => { asked.push('open:' + id); return target; };
    model.page = 'inbox';
    await model.goTo('personal', '#/p/prj_a/task/tsk_1');
    const frame = model.frames.personal;
    model.returnTo('inbox');
    expect(model.frames.personal).toBe(frame);
    expect(frame.epoch).toBeUndefined();
    expect(asked).toEqual(['open:personal']);
  });
});
