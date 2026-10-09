// The shell's state: which workspaces there are, which is open, which pages have been opened and what needs the person.
// Opening a workspace creates (once) a frame for it and keeps it, hidden when it is not the one shown, so that going to
// another workspace and back stops nothing in it. The model never starts, stops, approves or revokes anything in any
// workspace: those are asked of the app, by name, one at a time, in dialogs the person answers.

import { ask } from './native';
import type { Overview, Target, WorkspaceView } from './types';

export type Page = 'workspace' | 'mywork' | 'calendar' | 'inbox' | 'workspaces';

export interface OpenFrame {
  target: Target;
  /** A place to go to as soon as the page says it is ready. */
  pending?: string;
  ready: boolean;
}

const VIEW_EVERY = 4000;
const OVERVIEW_EVERY = 10000;

export class Model {
  view = $state<WorkspaceView | null>(null);
  overview = $state<Overview | null>(null);
  page = $state<Page>('workspace');
  /** The workspace the Workspace page shows. */
  current = $state('personal');
  frames = $state<Record<string, OpenFrame>>({});
  error = $state('');
  notice = $state('');
  busy = $state('');
  switcherOpen = $state(false);
  addOpen = $state(false);
  loaded = $state(false);

  private timers: ReturnType<typeof setInterval>[] = [];

  /** Starts the shell: opens the workspace the person left open, if they can still get into it, otherwise Personal. */
  async start(): Promise<void> {
    await this.refresh();
    const first = this.view?.selected ?? 'personal';
    await this.open(first).catch((e: Error) => (this.error = e.message));
    this.loaded = true;
    void this.refreshOverview();
    this.timers.push(
      setInterval(() => !document.hidden && void this.refresh(), VIEW_EVERY),
      setInterval(() => !document.hidden && void this.refreshOverview(), OVERVIEW_EVERY),
    );
    // An invitation the app was opened with is waiting: the add dialog hands it to the workspace it sets up.
    if (this.view?.invitation) this.addOpen = true;
  }

  stop(): void {
    this.timers.forEach(clearInterval);
    this.timers = [];
  }

  async refresh(): Promise<void> {
    try {
      const view = await ask((a) => a.Workspaces());
      const previous = this.view;
      this.view = view;
      for (const id of Object.keys(this.frames)) {
        const item = view.items.find(i => i.id === id);
        const old = previous?.items.find(i => i.id === id);
        const wasJoined = old && old.state !== 'setup';
        if (!item || item.state === 'leaving' || item.state === 'unavailable' || (item.state === 'setup' && wasJoined)) delete this.frames[id];
      }
      // Removed/revoked workspace content must disappear, including old aggregate rows.
      if (this.overview) this.overview.entries = this.accessibleEntries(this.overview);
      if (this.loaded && !this.frames[this.current] && this.current !== 'personal') await this.open('personal');
    } catch (e) {
      this.error = (e as Error).message;
    }
  }

  async refreshOverview(): Promise<void> {
    try {
      const overview = await ask((a) => a.Overview());
      // Membership may disappear while reads are in flight. Do not restore a retired workspace's old rows.
      this.overview = { ...overview, entries: this.accessibleEntries(overview) };
    } catch {
      // The overview is what is beside the work; a failure to read it is shown by its pages, not as an error here.
    }
  }

  private accessibleEntries(overview: Overview): Overview['entries'] {
    return overview.entries.filter(e => this.view?.items.some(i => i.id === e.workspace.id && i.state !== 'leaving' && i.state !== 'setup' && i.state !== 'unavailable'));
  }

  /** Opens a workspace on the Workspace page, and optionally somewhere inside it. */
  async open(id: string, place?: string): Promise<void> {
    this.error = '';
    try {
    const target = await ask((a) => a.OpenWorkspace(id));
    const existing = this.frames[id];
    if (!existing) {
      this.frames[id] = { target, ready: false, pending: place || target.place || undefined };
    } else if (place) {
      existing.pending = place;
    }
    this.current = id;
    this.page = 'workspace';
    this.switcherOpen = false;
    void this.refresh();
    } catch (e) { this.error = (e as Error).message; }
  }

  /** Marks a frame's page as loaded; where it should go next, if anywhere, is returned. */
  frameReady(id: string): string | undefined {
    const f = this.frames[id];
    if (!f) return undefined;
    f.ready = true;
    const p = f.pending;
    f.pending = undefined;
    return p;
  }

  /** Shows a page of the shell's own. */
  go(p: Page): void {
    this.page = p;
    this.switcherOpen = false;
    if (p === 'mywork' || p === 'calendar' || p === 'inbox' || p === 'workspaces') void this.refreshOverview();
  }

  /** Adds a Team: gives the person a place to create one or join one, and opens it. */
  async addTeam(): Promise<void> {
    this.busy = 'Opening Team…';
    this.error = '';
    try {
      const target = await ask((a) => a.AddTeam());
      this.frames[target.id] = { target, ready: false, pending: undefined };
      await this.refresh();
      this.current = target.id;
      this.page = 'workspace';
      this.switcherOpen = false;
      this.addOpen = false;
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.busy = '';
    }
  }

  /** Sets Team up on this Mac, after the app has asked the person. */
  async activateTeam(): Promise<void> {
    this.busy = 'Setting up Team…';
    this.error = '';
    try {
      await ask((a) => a.ActivateTeam());
      this.notice = 'Team is set up.';
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.busy = '';
      await this.refresh();
    }
  }

  async teamService(action: 'start' | 'stop' | 'uninstall'): Promise<void> {
    this.busy = 'Working…';
    this.error = '';
    try {
      await ask((a) => a.TeamService(action));
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.busy = '';
      await this.refresh();
    }
  }

  async connectRunner(id: string): Promise<void> {
    this.error = '';
    try {
      await ask((a) => a.Relay(id, 'ConnectRunner', []));
      this.notice = 'Your Personal runner is connected to this workspace.';
    } catch (e) {
      this.error = (e as Error).message;
    }
    await this.refresh();
    await this.refreshOverview();
  }

  /** Takes a workspace the person has left, or that never got set up, off the list. */
  async forget(id: string): Promise<void> {
    this.error = '';
    try {
      await ask((a) => a.ForgetWorkspace(id));
      delete this.frames[id];
      if (this.current === id) await this.open('personal');
    } catch (e) {
      this.error = (e as Error).message;
    }
    await this.refresh();
  }
}

export const model = new Model();
