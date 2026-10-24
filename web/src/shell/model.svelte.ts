// The shell's state: which workspaces there are, which is open, which pages have been opened and what needs the person.
// Opening a workspace creates (once) a frame for it and keeps it, hidden when it is not the one shown, so that going to
// another workspace and back stops nothing in it. The model never starts, stops, approves or revokes anything in any
// workspace: those are asked of the app, by name, one at a time, in dialogs the person answers.

import { shellPersonalAPI } from './personal';
import { generatedEntries } from './nav';
import type { Workspace } from '../lib/personal/schema';
import { ask } from './native';
import { surfaceUrl } from './frames';
import { frameOf, groupsFor, pick, scopeOf, sectionForPlace, type Group, type Scope, type Section } from './settings';
import type { Overview, Target, WorkspaceView } from './types';

export type Page = 'workspace' | 'calendar' | 'inbox' | 'workspaces' | 'settings' | 'help' | 'generated' | 'personal-library';

/** Where the switcher opens: under the workspace name at the top of the sidebar, in the window's coordinates. */
export interface Anchor {
  x: number;
  y: number;
  width: number;
  height: number;
}

const COLLAPSED_KEY = 'werkbord.shell.sidebar';

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === 'collapsed';
  } catch {
    return false;
  }
}

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
  page = $state<Page>('inbox');
  universal = $state(true);
  generated = $state<Workspace[]>([]);
  generatedID = $state('');
  generatedError = $state('');
  get visibleGenerated(): Workspace[] { return generatedEntries(this.generated, this.universal, this.current); }
  openGenerated(id: string, fromLibrary = false): void {
    if (!(fromLibrary ? this.generated : this.visibleGenerated).some(w => w.id === id)) return;
    this.leaveAssistant(); this.generatedID = id; this.page = 'generated'; this.closeSwitcher();
  }
  async followGenerated(id: string): Promise<void> {
    await this.refreshGenerated();
    const w = this.generated.find(w => w.id === id);
    if (!w) { this.error = 'This saved workspace is unavailable.'; return; }
    if (!this.visibleGenerated.some(w => w.id === id)) {
      if (w.visibility.universal) this.universal = true;
      else if (w.visibility.individual) { this.universal = false; this.current = 'personal'; }
      else {
        const team = this.view?.items.find(i => i.kind === 'team' && w.visibility.teamWorkspaceIds?.includes(i.id));
        if (!team) { this.error = 'None of this workspace’s display locations are available.'; return; }
        this.universal = false; this.current = team.id;
      }
    }
    this.openGenerated(id);
  }
  async refreshGenerated(): Promise<void> {
    try { this.generated = await shellPersonalAPI.workspaces(); this.generatedError = ''; }
    catch (e) { this.generated = []; this.generatedError = (e as Error).message; }
  }
  async customizeGenerated(id = '', revision?: number): Promise<void> {
    const rect = this.assistantButton?.getBoundingClientRect();
    await this.toggleAssistant(rect ?? {x: 24, y: 24, width: 0, height: 0}, id, revision);
  }

  /** The workspace the Workspace page shows. */
  current = $state('personal');
  frames = $state<Record<string, OpenFrame>>({});
  /**
   * A page of a workspace shown for Settings or Help, in a frame of its own, so that showing a workspace's settings stops
   * nothing in the workspace itself and leaves it where it was. Keyed by the workspace whose page it is.
   */
  hosted = $state<Record<string, OpenFrame>>({});
  /** The section of Settings on show (an id from settings.ts); one this scope does not list falls back to the first. */
  settingsSection = $state('');
  error = $state('');
  notice = $state('');
  busy = $state('');
  /** The open switcher, and where it opens; null when closed. */
  switcher = $state<Anchor | null>(null);
  /** The button the switcher opens under: the workspace name at the top of the sidebar. */
  switcherButton: HTMLElement | undefined;
  /** Where each open workspace says it is, as a place inside it: what the sidebar marks as current. */
  places = $state<Record<string, string>>({});
  sidebarCollapsed = $state(readCollapsed());
  addOpen = $state(false);
  loaded = $state(false);
  setupRequired = $state(false);
  setupPaused = $state(false);
  /** How Individual's assistant stands, as it last said: open over everything, something waiting for the person, a reply on the way. */
  assistant = $state({ open: false, waiting: 0, running: false });
  /** A request for the assistant, handed to its frame by AssistantFrame once the frame can take it (see toggleAssistant). */
  assistantAsk = $state<{ action: 'open' | 'close' | 'toggle'; rect: Anchor; personalWorkspace?: string; personalRevision?: number } | null>(null);
  /** The sidebar's Assistant button: the assistant opens out of it, and focus comes back to it. */
  assistantButton: HTMLElement | undefined;
  /**
   * The assistant's own frame: a page of Individual that is nothing but the assistant, as large as the window. It is made the first
   * time the assistant is opened and then kept, so that closing it stops nothing. It is not a workspace, and it is not in `frames`.
   */
  assistantFrame = $state<{ url: string; origin: string } | null>(null);
  /** Whether that frame is on screen (or about to be): it then covers the whole window, the sidebar included. */
  assistantShown = $state(false);
  /** An opening has been asked for and the frame has not yet said it is open. */
  private assistantOpening = false;
  private assistantWatch: ReturnType<typeof setTimeout> | undefined;

  private timers: ReturnType<typeof setInterval>[] = [];

  get switcherOpen(): boolean {
    return this.switcher !== null;
  }

  /** Opens the switcher under the workspace name in the sidebar (for the button and for the keyboard). */
  openSwitcher(): void {
    const r = this.switcherButton?.getBoundingClientRect();
    this.switcher = r ? { x: r.left, y: r.top, width: r.width, height: r.height } : { x: 8, y: 8, width: 0, height: 32 };
  }

  /** Closes the switcher; focus goes back to the name it opened under when the person pressed Escape. */
  closeSwitcher(focus = false): void {
    this.switcher = null;
    if (focus) this.switcherButton?.focus();
  }

  toggleSwitcher(): void {
    if (this.switcher) this.closeSwitcher(true);
    else this.openSwitcher();
  }

  /** Which part of Werkbord Settings is for: the whole of it, Individual, or the Team on show. */
  get scope(): Scope {
    return scopeOf(this.universal, this.current, this.view?.items.find((i) => i.id === this.current)?.kind);
  }

  get settingsGroups(): Group[] {
    return groupsFor(this.scope, this.view?.items);
  }

  get settingsShown(): Section {
    return pick(this.settingsGroups, this.settingsSection);
  }

  /** The workspace whose page is on screen now, in whichever frame shows it; undefined for the shell's own pages. */
  get shownWorkspace(): string | undefined {
    if (this.page === 'workspace') return this.current;
    if (this.page === 'help') return 'personal';
    if (this.page === 'settings') return frameOf(this.settingsShown);
    return undefined;
  }

  /** Records where a workspace's page says it is. */
  setPlace(id: string, place: string): void {
    if (this.frames[id]) this.places[id] = place;
  }

  /**
   * Opens or closes Individual's assistant. It is a frame of its own over the whole window, so nothing else is brought forward and
   * nothing underneath is touched: whatever was showing is still there, covered, when it closes. The frame is told where the button
   * is (in the window's coordinates, which are the frame's) so that it grows out of it and collapses back into it.
   */
  async toggleAssistant(rect: Anchor, personalWorkspace?: string, personalRevision?: number): Promise<void> {
    if (this.setupRequired && !this.setupPaused) { await this.open('personal', '#/onboarding'); return; }
    if (this.assistantShown && personalWorkspace === undefined) {
      this.assistantAsk = { action: 'close', rect };
      return;
    }
    this.error = '';
    if (!this.assistantFrame) {
      try {
        const target = await ask((a) => a.LoadWorkspace('personal'));
        this.assistantFrame = { url: surfaceUrl(target.url), origin: target.origin };
      } catch (e) {
        this.error = (e as Error).message;
        return;
      }
    }
    this.assistantShown = true;
    this.assistantOpening = true;
    // A frame that never answers must not be left over the window with nothing on it.
    clearTimeout(this.assistantWatch);
    this.assistantWatch = setTimeout(() => {
      this.assistantOpening = false;
      if (!this.assistant.open) this.assistantShown = false;
    }, 5000);
    this.assistantAsk = { action: 'open', rect, ...(personalWorkspace === undefined ? {} : { personalWorkspace, ...(personalRevision === undefined ? {} : { personalRevision }) }) };
  }

  /** The assistant's page says how it stands. When it has closed, the frame is put away and focus goes back to the button. */
  setAssistant(s: { open: boolean; waiting: number; running: boolean }): void {
    const was = this.assistant.open;
    this.assistant = s;
    if (s.open) {
      this.assistantOpening = false;
      clearTimeout(this.assistantWatch);
      this.assistantShown = true;
      return;
    }
    // Before the opening that was asked for begins, the page has only said that it is closed.
    if (this.assistantOpening) return;
    this.assistantShown = false;
    if (was) void this.refreshGenerated();
    if (was) queueMicrotask(() => this.assistantButton?.focus({ preventScroll: true }));
  }

  /** Closes the assistant if it is open, for a destination that is chosen some other way than through the sidebar it covers. */
  private leaveAssistant(): void {
    if (!this.assistantShown) return;
    const r = this.assistantButton?.getBoundingClientRect();
    this.assistantAsk = { action: 'close', rect: r ? { x: r.left, y: r.top, width: r.width, height: r.height } : { x: 24, y: 24, width: 0, height: 0 } };
  }

  /** Shows a place inside the workspace on show (a project, Settings…): the sidebar's way of navigating a workspace. */
  async visit(place: string): Promise<void> {
    this.universal = false;
    this.leaveAssistant();
    const frame = this.frames[this.current];
    if (!frame) return this.open(this.current, place);
    frame.pending = place;
    this.places[this.current] = place;
    this.page = 'workspace';
    this.closeSwitcher();
  }

  toggleSidebar(): void {
    this.sidebarCollapsed = !this.sidebarCollapsed;
    try {
      localStorage.setItem(COLLAPSED_KEY, this.sidebarCollapsed ? 'collapsed' : 'expanded');
    } catch {
      // Remembering is a convenience; the sidebar still works.
    }
  }

  /** Starts every window on Universal → Workspaces; workspace places remain available when explicitly opened. */
  async start(): Promise<void> {
    await this.refresh();
    this.page = 'inbox';
    this.universal = true;
    this.loaded = true;
    void this.refreshOverview();
    void this.refreshGenerated();
    // Load Individual in the background so first-use setup can identify itself before ordinary navigation.
    if (this.view?.items.some(i => i.id === 'personal' && i.state === 'ready')) {
      try {
        const target = await ask(a => a.LoadWorkspace('personal'));
        if (!this.frames.personal) this.frames.personal = { target, ready: false };
      } catch { /* The workspace problem view provides recovery when opened. */ }
    }
    this.timers.push(
      setInterval(() => !document.hidden && void this.refresh(), VIEW_EVERY),
      setInterval(() => !document.hidden && void this.refreshOverview(), OVERVIEW_EVERY),
      setInterval(() => !document.hidden && !this.assistantShown && void this.refreshGenerated(), VIEW_EVERY),
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
        if (!item || item.state === 'leaving' || item.state === 'unavailable' || (item.state === 'setup' && wasJoined)) {
          delete this.frames[id];
          delete this.places[id];
        }
      }
      for (const id of Object.keys(this.hosted)) {
        const item = view.items.find((i) => i.id === id);
        if (!item || item.state === 'leaving' || item.state === 'unavailable' || item.state === 'setup') delete this.hosted[id];
      }
      // Removed/revoked workspace content must disappear, including old aggregate rows.
      if (this.overview) this.overview.entries = this.accessibleEntries(this.overview);
      if (this.loaded && this.page === 'workspace' && !this.frames[this.current] && this.current !== 'personal') await this.open('personal');
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
    this.universal = false;
    if (id !== this.current) this.leaveAssistant();
    this.error = '';
    const item = this.view?.items.find((i) => i.id === id);
    if (id === 'personal' && item?.state === 'unavailable') {
      // There is no page to show: the Workspace page says why and what to do instead of a blank frame.
      delete this.frames.individual;
      delete this.places.individual;
      this.current = id;
      this.page = 'workspace';
      this.closeSwitcher();
      return;
    }
    try {
    const target = await ask((a) => a.OpenWorkspace(id));
    const existing = this.frames[id];
    if (!existing) {
      this.frames[id] = { target, ready: false, pending: place || target.place || undefined };
      this.places[id] = place || target.place || '';
    } else if (place) {
      existing.pending = place;
      this.places[id] = place;
    }
    this.current = id;
    this.page = 'workspace';
    this.closeSwitcher();
    void this.refresh();
    } catch (e) { this.error = (e as Error).message; }
  }

  /** Marks a frame's page as loaded; where it should go next, if anywhere, is returned. */
  frameReady(id: string, hosted = false): string | undefined {
    const f = hosted ? this.hosted[id] : this.frames[id];
    if (!f) return undefined;
    f.ready = true;
    const p = f.pending;
    f.pending = undefined;
    return p;
  }

  /** Shows a workspace's page in a frame of its own at a place inside it: for Settings and Help. */
  async host(id: string, place: string): Promise<void> {
    const open = this.hosted[id];
    if (open) {
      open.pending = place;
      return;
    }
    try {
      const target = await ask((a) => a.LoadWorkspace(id));
      // Another request may have made the frame while this one waited.
      if (this.hosted[id]) this.hosted[id].pending = place;
      else this.hosted[id] = { target, ready: false, pending: place };
    } catch (e) {
      this.error = (e as Error).message;
    }
  }

  /** Settings, at a section if one is asked for. Whichever part of Werkbord the person is in, it is the same screen. */
  openSettings(section?: string): void {
    this.leaveAssistant();
    if (section) this.settingsSection = section;
    this.page = 'settings';
    this.closeSwitcher();
    const where = this.settingsShown.where;
    if ('workspace' in where) void this.host(where.workspace, where.place);
  }

  /**
   * A page of a workspace asks for Settings at a place inside it (a link to Phones, a notice that points at Devices): the shell
   * shows its own Settings screen there, instead of the workspace growing a second one.
   */
  openSettingsAt(workspace: string, place: string): void {
    if (this.view?.items.find((i) => i.id === workspace)?.kind === 'team') this.universal = false;
    else if (workspace === 'personal') this.universal = false;
    this.current = workspace;
    const found = sectionForPlace(this.settingsGroups, workspace, place);
    this.openSettings(found?.id);
  }

  /** Help: Individual's guides and shortcuts, shown wherever the person is. */
  openHelp(): void {
    this.leaveAssistant();
    this.page = 'help';
    this.closeSwitcher();
    void this.host('personal', '#/help');
  }

  /** Shows a page of the shell's own. */
  go(p: Page): void {
    if (p === 'settings') return this.openSettings();
    if (p === 'help') return this.openHelp();
    if (['calendar', 'inbox'].includes(p)) this.universal = true;
    this.leaveAssistant();
    this.page = p;
    this.closeSwitcher();
    if (p === 'calendar' || p === 'inbox' || p === 'workspaces') void this.refreshOverview();
  }

  /** Adds a Team: gives the person a place to create one or join one, and opens it. */
  async addTeam(): Promise<void> {
    this.busy = 'Opening Team…';
    this.error = '';
    try {
      const target = await ask((a) => a.AddTeam());
      this.frames[target.id] = { target, ready: false, pending: undefined };
      this.places[target.id] = target.place || '';
      await this.refresh();
      this.current = target.id;
      this.universal = false;
      this.page = 'workspace';
      this.closeSwitcher();
      this.addOpen = false;
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.busy = '';
    }
  }

  /** Sets Team up on this Mac, or updates its service, after the app has asked the person. */
  async activateTeam(): Promise<void> {
    const updating = this.view?.team.state === 'outdated';
    this.busy = updating ? 'Updating Team…' : 'Setting up Team…';
    this.error = '';
    try {
      await ask((a) => a.ActivateTeam());
      this.notice = updating ? 'Team is updated.' : 'Team is set up.';
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.busy = '';
      await this.refresh();
    }
  }

  async teamService(action: 'start' | 'stop' | 'uninstall'): Promise<void> {
    this.busy = action === 'start' ? 'Starting Team service…' : action === 'stop' ? 'Stopping Team service…' : 'Removing Team service…';
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
      this.notice = 'Your Individual runner is connected to this workspace.';
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
      delete this.places[id];
      if (this.current === id) await this.open('personal');
    } catch (e) {
      this.error = (e as Error).message;
    }
    await this.refresh();
  }
}

export const model = new Model();
