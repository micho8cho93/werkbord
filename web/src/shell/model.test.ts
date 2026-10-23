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
    expect(model.page).toBe('mywork');
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
    model.go('mywork');
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
      expect(model.page).toBe('mywork');
      expect(model.universal).toBe(true);
      expect(open).not.toHaveBeenCalled();
      expect(Object.keys(model.frames)).toEqual(['personal']); // Background readiness check, without selecting it.
      await model.refresh();
      expect(model.page).toBe('mywork');
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
  expect(model.page).toBe('mywork');
  model.openGenerated(saved.id, true);
  expect(model.page).toBe('generated');
  expect(model.generatedID).toBe(saved.id);
  expect(model.visibleGenerated).toEqual([]);
  expect(model.generated[0].visibility).toEqual(saved.visibility);
});
