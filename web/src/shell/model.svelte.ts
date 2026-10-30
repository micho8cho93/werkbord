// The shell's state: which workspaces there are (Personal, and each team), which is open, which pages have been opened and what needs the person.
// Opening a workspace creates (once) a frame for it and keeps it, hidden when it is not the one shown, so that going to
// another workspace and back stops nothing in it. The model never starts, stops, approves or revokes anything in any
// workspace: those are asked of the app, by name, one at a time, in dialogs the person answers.

import { ask } from './native';
import { contextKey, parseContext, projectOptions, projectInHref, scopedOverview, type ProjectIdentity } from './project-context';
import { surfaceUrl } from './frames';
import { frameOf, groupsFor, pick, scopeOf, sectionForPlace, type Group, type Scope, type Section } from './settings';
import type { App, Overview, Target, WorkspaceView } from './types';
import { readWanted, writeWanted, type TeamSetupProgress } from './teamsetup';

export type Page = 'workspace' | 'calendar' | 'inbox' | 'reviews' | 'analytics' | 'workspaces' | 'settings' | 'help' | 'agents';

/** Where the assistant grows out of: the button that opened it, in the window's coordinates. */
export interface Anchor {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface OpenFrame {
  target: Target;
  /** A place to go to as soon as the page says it is ready. */
  pending?: string;
  ready: boolean;
  /** Raised to load the page again, as a new frame (the same address would not reload one). */
  epoch?: number;
}

/** A page refused its sign-in again within this long of the app signing it in again: signing in again is not the answer. */
const REAUTH_EVERY = 30000;

const VIEW_EVERY = 4000;
const OVERVIEW_EVERY = 10000;

export class Model {
  view = $state<WorkspaceView | null>(null);
  overview = $state<Overview | null>(null);
  page = $state<Page>('inbox');
  selectedProject = $state<ProjectIdentity | null>((() => {
    try { return parseContext(localStorage.getItem('werkbord.shell.project') ?? ''); } catch { return null; }
  })());
  get projects() { return projectOptions(this.view?.items ?? [], this.overview); }
  get contextOverview(): Overview | null { return scopedOverview(this.overview, this.view?.items ?? [], this.selectedProject); }
  get contextLabel(): string {
    if (!this.selectedProject) return 'All Projects';
    const p = this.projects.find(p => contextKey(p) === contextKey(this.selectedProject));
    return p ? `${p.name} · ${p.owner}` : 'Project unavailable';
  }
  selectProject(selected: ProjectIdentity | null): void {
    this.selectedProject = selected;
    try { localStorage.setItem('werkbord.shell.project', contextKey(selected)); } catch { /* Selection still works. */ }
    // Detail screens are contextual Work; switching projects returns to the shared Work surface.
    if (this.page === 'workspace') this.go('inbox');
  }

  /**
   * A page of the window's own (Home, Calendar, Devices & Hosting) is on show rather than a workspace's page. It
   * is not a mode the person chooses: there is one sidebar, and this only says which kind of place they are in.
   */
  universal = $state(true);
  get remotePersonal(): boolean { return this.view?.items.find(i => i.id === 'personal')?.deviceRoles?.includes('control_client') ?? false; }
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
  /** The go-to palette (⌘K) is open. */
  paletteOpen = $state(false);
  /** Where each open workspace says it is, as a place inside it: what the sidebar marks as current. */
  places = $state<Record<string, string>>({});
  addOpen = $state(false);
  loaded = $state(false);
  setupRequired = $state(false);
  setupPaused = $state(false);
  /** How Individual's assistant stands, as it last said: open over everything, something waiting for the person, a reply on the way. */
  assistant = $state<{open: boolean; waiting: number; running: boolean; compact?: boolean}>({ open: false, waiting: 0, running: false });
  /** A request for the assistant, handed to its frame by AssistantFrame once the frame can take it (see toggleAssistant). */
  assistantAsk = $state<{ action: 'open' | 'close' | 'toggle' | 'compact'; run?: {projectId: string; runId: string}; rect: Anchor; catchUpPrompt?: string } | null>(null);
  /** The launcher used most recently; preference changes can remove either launcher. */
  private lastAssistantButton: HTMLElement | undefined;
  assistantSidebarButton: HTMLElement | undefined;
  assistantFloatingButton: HTMLElement | undefined;
  get assistantButton(): HTMLElement | undefined {
    return [this.lastAssistantButton, this.assistantFloatingButton, this.assistantSidebarButton].find(button => button && button.isConnected !== false);
  }
  set assistantButton(button: HTMLElement | undefined) { this.lastAssistantButton = button; }
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

  openPalette(): void {
    this.paletteOpen = true;
  }

  closePalette(): void {
    this.paletteOpen = false;
  }

  togglePalette(): void {
    this.paletteOpen = !this.paletteOpen;
  }

  /** Which part of Werkbord Settings is for: the whole of it, Personal, or the team on show. */
  get scope(): Scope {
    return scopeOf(this.universal, this.current, this.view?.items.find((i) => i.id === this.current)?.kind);
  }

  get settingsGroups(): Group[] {
    return groupsFor(this.scope, this.view?.items)
      .map(g => ({...g, sections: g.sections.filter(s => !this.remotePersonal || !s.id.startsWith('personal/') || s.id === 'personal/runners')}))
      .filter(g => g.sections.length);
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
    if (this.page === 'workspace' && this.current === id) {
      const kind = this.view?.items.find(i => i.id === id)?.kind;
      const project = kind && projectInHref(kind, place);
      if (project && this.projects.some(p => p.workspace === id && p.project === project)) this.selectProjectFromRoute(id, project);
    }
  }

  private selectProjectFromRoute(workspace: string, project: string): void {
    this.selectedProject = { workspace, project };
    try { localStorage.setItem('werkbord.shell.project', contextKey(this.selectedProject)); } catch { /* Optional persistence. */ }
  }

  /**
   * Opens or closes Individual's assistant. It is a frame of its own over the whole window, so nothing else is brought forward and
   * nothing underneath is touched: whatever was showing is still there, covered, when it closes. The frame is told where the button
   * is (in the window's coordinates, which are the frame's) so that it grows out of it and collapses back into it.
   */
  async prepareAssistant(): Promise<void> {
    if (this.remotePersonal) return;
    if (this.assistantFrame) return;
    const target = await ask(a => a.LoadWorkspace('personal'));
    this.assistantFrame = { url: surfaceUrl(target.url), origin: target.origin };
  }
  async openRunSession(projectId: string, runId: string): Promise<void> {
    if (this.remotePersonal) { await this.open('personal', `#/p/${encodeURIComponent(projectId)}/runs`); return; }
    try { await this.prepareAssistant(); }
    catch (e) { this.error = (e as Error).message; return; }
    this.assistantShown = true;
    this.assistantOpening = true;
    clearTimeout(this.assistantWatch);
    this.assistantWatch = setTimeout(() => {
      this.assistantOpening = false;
      if (!this.assistant.open) this.assistantShown = false;
    }, 5000);
    this.assistantAsk = { action: 'open', run: { projectId, runId }, rect: { x: 24, y: 24, width: 0, height: 0 } };
  }
  async toggleAssistant(rect: Anchor): Promise<void> {
    if (this.remotePersonal) return;
    if (this.setupRequired && !this.setupPaused) { await this.open('personal', '#/onboarding'); return; }
    if (this.assistantShown) {
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
    this.assistantAsk = { action: 'open', rect };
  }

  async explainCatchUp(prompt: string): Promise<void> {
    await this.toggleAssistant({ x: 24, y: 24, width: 0, height: 0 });
    if (this.assistantAsk) this.assistantAsk = { action: 'open', rect: this.assistantAsk.rect, catchUpPrompt: prompt.slice(0, 24000) };
  }

  /** The assistant's page says how it stands. When it has closed, the frame is put away and focus goes back to the button. */
  setAssistant(s: { open: boolean; waiting: number; running: boolean; compact?: boolean }): void {
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
  }

  /**
   * Shows a place in a workspace, whichever one is open now: the sidebar's way of reaching a project or a team's people. A workspace
   * that already has a page keeps it (nothing in it restarts); otherwise it is opened.
   */
  async goTo(id: string, place?: string): Promise<void> {
    if (place && id === this.current && this.frames[id]) return this.visit(place);
    return this.open(id, place);
  }

  /** Starts every window on Work; workspace places remain available when explicitly opened. */
  async start(): Promise<void> {
    await this.refresh();
    this.page = 'inbox';
    this.universal = true;
    this.loaded = true;
    void this.refreshOverview();
    // Load Personal in the background so first-use setup can identify itself before ordinary navigation.
    if (this.view?.items.some(i => i.id === 'personal' && i.state === 'ready')) {
      try {
        const target = await ask(a => a.LoadWorkspace('personal'));
        if (!this.frames.personal) this.frames.personal = { target, ready: false };
      } catch { /* The workspace problem view provides recovery when opened. */ }
    }
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
      this.settleSetup(view);
      if (this.selectedProject && !view.items.some(i => i.id === this.selectedProject?.workspace && !['setup', 'leaving'].includes(i.state))) this.selectProject(null);
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

  async refreshOverview(): Promise<boolean> {
    try {
      const overview = await ask((a) => a.Overview());
      // Membership may disappear while reads are in flight. Do not restore a retired workspace's old rows.
      this.overview = { ...overview, entries: this.accessibleEntries(overview) };
      const selected = this.selectedProject;
      const source = selected && this.overview.entries.find(e => e.workspace.id === selected.workspace);
      if (selected && source?.summary && !source.error && this.view?.items.find(i => i.id === selected.workspace)?.state === 'ready' && !source.summary.projects.some(p => p.id === selected.project)) {
        this.selectProject(null);
        this.notice = 'The selected project is no longer accessible. Showing All Projects.';
      }
      return true;
    } catch {
      // Keep project labels for recovery, but never present old records as a current authorised aggregate.
      const previous = this.overview ?? { at: '', entries: (this.view?.items ?? []).map(workspace => ({ workspace })) };
      this.overview = { ...previous, entries: previous.entries.map(e => ({ ...e, error: 'Workspace summaries could not be refreshed. Reconnect to try again.' })) };
      return false;
    }
  }

  private accessibleEntries(overview: Overview): Overview['entries'] {
    return overview.entries.filter(e => this.view?.items.some(i => i.id === e.workspace.id && i.state !== 'leaving' && i.state !== 'setup' && i.state !== 'unavailable'));
  }

  /** Opens a workspace on the Workspace page, and optionally somewhere inside it. */
  async open(id: string, place?: string): Promise<void> {
    const kind = this.view?.items.find(i => i.id === id)?.kind;
    const project = kind && place && projectInHref(kind, place);
    if (project && this.projects.some(p => p.workspace === id && p.project === project)) this.selectProjectFromRoute(id, project);
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

  private reauthAt: Record<string, number> = {};

  /**
   * A workspace's page was refused its sign-in. Asks the app for the page's address as it is now (it reads the token again) and
   * loads the page again at the place it was. Returns why that could not be done, or null when it was; a page that is refused
   * again straight after is not given the same answer in a circle.
   */
  async reauth(id: string, manual = false): Promise<string | null> {
    const now = Date.now();
    if (!manual && now - (this.reauthAt[id] ?? 0) < REAUTH_EVERY) {
      return 'Signing in again a moment ago did not help: Werkbord is not accepting this app’s access token.';
    }
    this.reauthAt[id] = now;
    let target: Target;
    try {
      target = await ask((a) => a.LoadWorkspace(id));
    } catch (e) {
      return (e as Error).message || 'The app could not read Werkbord’s access token.';
    }
    const main = this.frames[id];
    if (main) {
      main.target = target;
      main.ready = false;
      main.pending = this.places[id] || main.pending;
      main.epoch = (main.epoch ?? 0) + 1;
    }
    const settings = this.hosted[id];
    if (settings) {
      settings.target = target;
      settings.ready = false;
      settings.epoch = (settings.epoch ?? 0) + 1;
    }
    if (settings && this.page === 'settings') this.openSettings();
    return null;
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
    void this.host('personal', '#/help');
  }

  /** Shows a page of the shell's own. */
  go(p: Page): void {
    if (p === 'settings') return this.openSettings();
    if (p === 'help') return this.openHelp();
    if (['calendar', 'inbox', 'reviews', 'analytics', 'agents'].includes(p)) this.universal = true;
    this.leaveAssistant();
    this.page = p;
    if (p === 'calendar' || p === 'inbox' || p === 'reviews' || p === 'analytics' || p === 'agents' || p === 'workspaces') void this.refreshOverview();
  }

  /** The new-team dialog's work, once it has started: kept here so that closing the dialog does not lose it. */
  setup = $state<TeamSetupProgress | null>(null);
  private setupRun = 0;
  /** How long to wait between looks at a join that waits for an administrator (a test shortens it). */
  waitingPause = 5000;

  /** Starts creating a team hosted on this computer. Team's service is set up first, with the person's say-so, when it is not. */
  async createTeam(name: string, owner: string, connect: boolean): Promise<void> {
    await this.startSetup('create', connect, (a) => a.CreateTeam(name, owner), name.trim());
  }

  /** Starts joining the team an invitation is for. */
  async joinTeam(link: string, name: string, device: string, connect: boolean, team = ''): Promise<void> {
    await this.startSetup('join', connect, (a) => a.JoinTeam(link, name, device), team);
  }

  private teamReady(): boolean {
    return !this.view || this.view.team.state === 'ready';
  }

  private async startSetup(kind: 'create' | 'join', connect: boolean, start: (a: App) => Promise<{ id: string }>, team: string): Promise<void> {
    this.error = '';
    if (!this.teamReady()) {
      await this.activateTeam();
      if (!this.teamReady()) return; // the person said no, or the installer refused: it has said why
    }
    this.busy = kind === 'create' ? 'Creating your team…' : 'Joining the team…';
    try {
      const { id } = await ask(start);
      this.setup = { id, kind, connect, working: this.busy, waiting: false, exists: false, error: '', name: team };
      void this.followSetup(id);
    } catch (e) {
      this.error = (e as Error).message;
    } finally {
      this.busy = '';
    }
  }

  /** Watches the service make the team, until it is done, waits for an administrator, or fails. */
  private async followSetup(id: string): Promise<void> {
    const run = ++this.setupRun;
    const give = Date.now() + 5 * 60_000;
    while (run === this.setupRun && this.setup?.id === id) {
      let v;
      try {
        v = await ask((a) => a.TeamSetupState(id));
      } catch (e) {
        if (run === this.setupRun && this.setup) this.setup.error = (e as Error).message;
        return;
      }
      if (run !== this.setupRun || !this.setup) return;
      // An administrator has to approve this computer, which can take days: remember the wish to connect the runner for then.
      if (v.waiting && this.setup.connect) writeWanted([...readWanted().filter((w) => w !== id), id]);
      this.setup.name = v.name || this.setup.name;
      this.setup.working = v.working;
      this.setup.waiting = v.waiting;
      this.setup.exists = !!v.exists;
      if (v.error) {
        this.setup.error = v.error;
        return;
      }
      if (v.done) return void (await this.finishSetup(id));
      if (v.waiting) {
        // An administrator has to approve this computer, which can take days: keep looking, slowly, for as long as the window is open.
        await this.refresh();
        await new Promise((r) => setTimeout(r, this.waitingPause));
        continue;
      }
      if (Date.now() > give) {
        this.setup.error = 'Team is taking longer than expected. It carries on in the background: the team appears in the sidebar when it is ready.';
        return;
      }
      await new Promise((r) => setTimeout(r, 1500));
    }
  }

  private finishing: string[] = [];

  private async finishSetup(id: string): Promise<void> {
    const s = this.setup;
    if (!s || s.id !== id || this.finishing.includes(id)) return;
    this.finishing.push(id);
    writeWanted(readWanted().filter((w) => w !== id)); // asked once, here, and not again from the list
    await this.refresh();
    let notice = s.kind === 'create' ? `${s.name || 'Your team'} is ready.` : `You joined ${s.name || 'the team'}.`;
    if (s.connect) {
      try {
        await ask((a) => a.Relay(id, 'ConnectRunner', []));
        notice += ' Your own runner is connected.';
      } catch (e) {
        notice += ` Your runner was not connected (${(e as Error).message}); you can connect it from the team’s page.`;
      }
    }
    this.setup = null;
    this.addOpen = false;
    this.notice = notice;
    this.finishing = this.finishing.filter((f) => f !== id);
    await this.open(id);
    await this.refreshOverview();
  }

  /** Leaves what the dialog was doing: a join that waits is withdrawn and a failed one cleared; work in progress is left to finish. */
  async cancelSetup(): Promise<void> {
    const s = this.setup;
    this.setupRun++;
    this.setup = null;
    if (!s) return;
    writeWanted(readWanted().filter((w) => w !== s.id));
    if (s.exists) this.addOpen = false; // the team stays; it is left from its own page
    if (s.working || s.exists) return;
    try {
      await ask((a) => a.CancelTeamSetup(s.id));
    } catch (e) {
      this.error = (e as Error).message;
    }
    await this.refresh();
  }

  /**
   * A person who asked to connect their runner before an administrator let them in is asked once, when the team is connected. While the
   * dialog's own watcher is following that team (setup), the watcher does it; this is for a window that was closed and reopened since.
   */
  private settleSetup(view: WorkspaceView): void {
    const wanted = readWanted();
    if (!wanted.length) return;
    const following = this.setup?.id;
    const ready = wanted.filter((id) => id !== following && view.items.some((i) => i.id === id && i.state === 'ready'));
    const gone = wanted.filter((id) => id !== following && !view.items.some((i) => i.id === id));
    if (!ready.length && !gone.length) return;
    writeWanted(wanted.filter((id) => !ready.includes(id) && !gone.includes(id)));
    for (const id of ready) {
      void ask((a) => a.Relay(id, 'ConnectRunner', []))
        .then(() => this.refreshOverview())
        .catch((e: Error) => { this.notice = `Your runner was not connected (${e.message}); you can connect it from the team’s page.`; });
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
      this.notice = 'Your own runner is connected to this team.';
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
