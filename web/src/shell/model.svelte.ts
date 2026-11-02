// The shell's state: which workspaces there are (Personal, and each team), which is open, which pages have been opened and what needs the person.
// Opening a workspace creates (once) a frame for it and keeps it, hidden when it is not the one shown, so that going to
// another workspace and back stops nothing in it. The model never starts, stops, approves or revokes anything in any
// workspace: those are asked of the app, by name, one at a time, in dialogs the person answers.

import { SvelteSet, SvelteURLSearchParams } from 'svelte/reactivity';
import { decisions } from './decisions.svelte';
import { alerts } from './alerts.svelte';
import { viewState } from './view-state.svelte';
import { ask } from './native';
import { contextKey, parseContext, projectOptions, projectInHref, scopedOverview, type ProjectIdentity } from './project-context';
import { surfaceUrl, type RepoChange } from './frames';
import { agentsPlace, isAgentsPlace, scopedAgentsPlace } from './nav';
import { chainOf, pushed, withChain, type Origin } from '../lib/origin';
import { frameOf, groupsFor, pick, scopeOf, sectionForPlace, type Group, type Scope, type Section } from './settings';
import type { App, Item, Overview, Target, WorkspaceView } from './types';
import { readWanted, writeWanted, type TeamSetupProgress } from './teamsetup';

export type Page = 'workspace' | 'calendar' | 'git' | 'inbox' | 'reviews' | 'analytics' | 'workspaces' | 'settings' | 'help' | 'agents';

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

/** The narrowest window (CSS px) in which a list and a detail fit side by side. */
export const PANE_MIN = 1000;

const VIEW_EVERY = 4000;
const OVERVIEW_EVERY = 10000;
/** How many answers in a row may be missed before the work they would have refreshed is taken off the screen (about half a minute). */
const MISSES_BEFORE_WITHDRAWN = 3;
/** How many looks in a row may show a workspace unreachable before it is said to be: a network blip lasts a look or two. */
const LOOKS_BEFORE_UNREACHABLE = 3;

export class Model {
  view = $state<WorkspaceView | null>(null);
  overview = $state<Overview | null>(null);
  page = $state<Page>('inbox');
  /** What the page in each workspace's frame says it is showing (a plan's or a task's title), for the header's breadcrumb. Empty when it shows none. */
  private frameTitles = $state<Record<string, string>>({});
  setFrameTitle(id: string, title: string): void { if ((this.frameTitles[id] ?? '') !== title) this.frameTitles = { ...this.frameTitles, [id]: title }; }
  /** The title the workspace on show gave its page, only while that workspace's page is what the person is looking at. */
  get frameTitle(): string { return this.page === 'workspace' ? this.frameTitles[this.current] ?? '' : ''; }
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
    this.pane = null; // a task of the old project must not stay beside the list of the new one
    try { localStorage.setItem('werkbord.shell.project', contextKey(selected)); } catch { /* Selection still works. */ }
    // Detail screens are contextual Work; switching projects returns to the shared Work surface. A person's own project's Work
    // (its queue and plans, which can be reordered) and its Git stay where they are, on the new project.
    if (this.page === 'workspace' && this.onGit && selected) void this.openGit();
    else if (this.page === 'workspace' && this.onProjectWork && selected) this.openProjectWork();
    else if (this.page === 'git') void this.openGit();
    else if (this.page === 'workspace') this.go('inbox');
    // Agents lists the sessions of the project context: the list follows it, and an open session stays open.
    else if (this.agentsInFrame && this.frames.personal) void this.openAgents();
    this.departed = null;
  }

  /**
   * A page of the window's own (Home, Calendar, Devices & Hosting) is on show rather than a workspace's page. It
   * is not a mode the person chooses: there is one sidebar, and this only says which kind of place they are in.
   */
  universal = $state(true);
  /**
   * The team this computer already hosts, if it does. A computer can host only one Team workspace (its Host listens on fixed ports), so a
   * second team cannot be created here; the person is told so before they fill anything in, and can still join teams hosted elsewhere.
   */
  get hostedHere(): Item | undefined {
    return this.view?.items.find(i => i.kind === 'team' && i.state !== 'setup' && (i.deviceRoles ?? this.overview?.entries.find(e => e.workspace.id === i.id)?.summary?.workspace.deviceRoles ?? []).includes('workspace_host'));
  }
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
  /**
   * Where the person left one of the window's own pages (Work, Calendar, Agents) for a detail page, and the project context they
   * had there. Back to that page puts both as they were, even if opening the detail moved the context to its project.
   */
  departed = $state<{ page: Page; context: ProjectIdentity | null; workspace: string; place: string; from?: { workspace: string; place: string | undefined; label: string } } | null>(null);
  /**
   * Work's detail pane: a task, plan or session shown beside the list rather than instead of it, so the list keeps its place, its search
   * and its scroll, and Back is closing the pane. `place` is the queue's own address for what was opened, which is what the list marks.
   */
  pane = $state<{ workspace: string; place: string } | null>(null);
  /** The window is wide enough for the list and a detail side by side; a narrower one gives the detail the whole page, as it always did. */
  wide = $state(typeof window === 'undefined' ? true : window.innerWidth >= PANE_MIN);
  addOpen = $state(false);

  /** The "Where should it live?" dialog of New project. */
  newProjectOpen = $state(false);
  /** A workspace whose New project form is on show, so the project it makes is chosen as soon as the window learns of it. */
  private creatingIn = '';
  /** Where a project can be made: the person's own Werkbord, and every team that answers. */
  get projectHomes(): Item[] {
    return (this.view?.items ?? []).filter((i) => i.state === 'ready' && (i.kind === 'personal' || i.kind === 'team'));
  }
  /**
   * New project, from anywhere. With one place to make it (only Personal) it goes straight to the form; with teams as well the person
   * says where it lives first, because it cannot be moved afterwards without a transfer.
   */
  newProject(): void {
    const homes = this.projectHomes;
    if (homes.length === 1) void this.startNewProject(homes[0].id);
    else if (homes.length > 1) this.newProjectOpen = true;
    else this.error = 'No workspace is ready to hold a project yet. Wait for Personal or your team to connect, then try again.';
  }
  /** Shows the form that makes a project in that workspace, with nothing else around it. */
  async startNewProject(id: string): Promise<void> {
    this.newProjectOpen = false;
    this.creatingIn = id;
    await this.open(id, id === 'personal' ? '#/projects?new=project' : '?tab=projects&new=project');
  }
  loaded = $state(false);
  setupRequired = $state(false);
  setupPaused = $state(false);
  /** How Individual's assistant stands, as it last said: open over everything, something waiting for the person, a reply on the way. */
  assistant = $state<{open: boolean; waiting: number; running: boolean}>({ open: false, waiting: 0, running: false });
  /** A request for the assistant, handed to its frame by AssistantFrame once the frame can take it (see toggleAssistant). */
  assistantAsk = $state<{ action: 'open' | 'close' | 'toggle'; conversation?: string; rect: Anchor; catchUpPrompt?: string; sendNow?: boolean } | null>(null);
  /** The sidebar's Salie destination: the one place Salie opens from, and where focus returns when it closes. */
  assistantSidebarButton: HTMLElement | undefined;
  get assistantButton(): HTMLElement | undefined {
    return this.assistantSidebarButton?.isConnected === false ? undefined : this.assistantSidebarButton;
  }
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
    if (this.paneOpen) return this.pane?.workspace;
    if (this.page === 'workspace') return this.current;
    if (this.agentsInFrame) return 'personal';
    if (this.page === 'help') return 'personal';
    if (this.page === 'settings') return frameOf(this.settingsShown);
    return undefined;
  }

  /** Work is showing a task or session beside its list. */
  get paneOpen(): boolean {
    return this.page === 'inbox' && !!this.pane && this.wide;
  }
  /** A workspace's page (not a hosted Settings or Help page) is what the window's main frame shows now. */
  get framePage(): boolean {
    return this.page === 'workspace' || this.agentsInFrame || this.paneOpen;
  }
  /** Whether Work can open something beside its list: the window is wide enough and Personal or a team can be asked for the page. */
  get paneable(): boolean {
    return this.page === 'inbox' && this.wide;
  }

  /**
   * Opens a task, plan or session of a workspace beside Work's list. The workspace's page is made if it is not there and told where to
   * go; nothing about the window's project context or page changes, so closing the pane is exactly Back.
   */
  async openPane(id: string, place: string): Promise<void> {
    if (!this.paneable) return this.goTo(id, place);
    const item = this.view?.items.find((i) => i.id === id);
    if (!item || item.state === 'unavailable') return this.goTo(id, place);
    this.error = '';
    const where = this.stamped(id, place) ?? place;
    try {
      const target = await ask((a) => a.OpenWorkspace(id));
      const existing = this.frames[id];
      if (!existing) {
        this.frames[id] = { target, ready: false, pending: where };
      } else {
        existing.pending = where;
      }
      this.places[id] = where;
      this.pane = { workspace: id, place };
    } catch (e) { this.error = (e as Error).message; }
  }

  /** Puts the pane away: Work is its list alone again, as it was left. */
  closePane(): void {
    this.pane = null;
    this.departed = null;
  }

  /** The window was resized across the width a pane needs. A pane that no longer fits becomes the page it would have been. */
  setWide(wide: boolean): void {
    if (wide === this.wide) return;
    this.wide = wide;
    const open = this.pane;
    if (!wide && open && this.page === 'inbox') {
      this.pane = null;
      void this.goTo(open.workspace, this.places[open.workspace] || open.place);
    }
  }

  /** Records where a workspace's page says it is. */
  setPlace(id: string, place: string): void {
    if (this.frames[id]) this.places[id] = place;
    // A Team page that moved to another tab is no longer the ticket the return bar was shown for.
    if (this.departed && this.departed.workspace === id && id !== 'personal' && place.startsWith('?')) {
      const ticketOf = (p: string) => /[?&]ticket=([^&]*)/.exec(p)?.[1];
      const wanted = ticketOf(this.departed.place);
      if (wanted && ticketOf(place) !== wanted) this.departed = null;
    }
    // The Individual page leaves Agents (a session's task, say): the window is then on Work, and the sidebar says so.
    if (id === 'personal' && !this.frames.personal?.pending) {
      if (this.page === 'agents' && !isAgentsPlace(place)) { this.current = 'personal'; this.universal = false; this.page = 'workspace'; }
      else if (this.page === 'workspace' && this.current === 'personal' && isAgentsPlace(place)) { this.universal = true; this.page = 'agents'; }
    }
    if (this.page === 'workspace' && this.current === id) {
      const kind = this.view?.items.find(i => i.id === id)?.kind;
      const project = kind && projectInHref(kind, place);
      if (project && this.projects.some(p => p.workspace === id && p.project === project)) this.selectProjectFromRoute(id, project);
      // The form just made a project the window has not been told about yet: ask, then choose it.
      else if (project && this.creatingIn === id) {
        this.creatingIn = '';
        void this.refreshOverview().then(() => {
          if (this.projects.some(p => p.workspace === id && p.project === project)) this.selectProjectFromRoute(id, project);
        });
      }
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
  /** Where the brief's frame loads from: the person's own Werkbord, as the page that shows one write-up and nothing else. */
  briefFrame = $state<{ url: string; origin: string } | null>(null);
  async prepareBrief(): Promise<void> {
    if (this.remotePersonal || this.briefFrame || !this.personalReady) return;
    const target = await ask(a => a.LoadWorkspace('personal'));
    this.briefFrame = { url: surfaceUrl(target.url, 'brief'), origin: target.origin };
  }
  /** Whether the person's own Werkbord is there to show Agents in. */
  get personalReady(): boolean {
    return this.view?.items.find(i => i.id === 'personal')?.state === 'ready';
  }
  /**
   * Agents: every coding-agent session, and the one that is open. It is a page of the person's own Werkbord shown in its frame
   * (the one terminal and session views live there), so opening it, or opening a session in it, stops and starts nothing. A
   * session is named by its key (agent-sessions.ts). With no key Agents comes back to where it was, a session still open. `beside` opens
   * it next to Work's list (see `pane`) when the window is wide enough.
   */
  async openAgents(key?: string, from: readonly Origin[] = [], beside = false): Promise<void> {
    // Opened from Salie: remember the page she was covering, for when she is closed again.
    if (this.assistantShown) { this.salieReturn = this.snapshot(); this.salieBack = false; }
    if (!this.personalReady) {
      // Without the person's own Werkbord there are no sessions to open: the page lists what workspaces report.
      this.leaveAssistant();
      this.universal = true;
      this.page = 'agents';
      void this.refreshOverview();
      return;
    }
    this.leaveAssistant();
    // A session opened from one of the window's own pages remembers it; one opened from a page that says where it is from does not guess.
    const origin = key && !from.length && this.here && this.here.kind !== 'agents' ? pushed(this.here, []) : [...from];
    // From Work's own controls (`beside`), or from a page already open beside the list, on a window wide enough, the session opens beside
    // the list and the person's place in Work is kept. Asked for from anywhere else (Salie, say) it is the Agents page.
    if (key && this.paneable && (beside || this.pane)) {
      const scoped = this.selectedProject?.workspace === 'personal' ? this.selectedProject.project : '';
      const place = agentsPlace(key, scoped, origin);
      return this.openPane('personal', place + (place.includes('?') ? '&' : '?') + 'pane=1');
    }
    // The window is on Agents at once; its frame follows as soon as the person's own Werkbord has been asked for it.
    this.universal = true;
    this.page = 'agents';
    const kept = this.places.personal;
    // Agents is scoped by the window's project context (a Personal project), so the list is not asked about twice.
    const project = this.selectedProject?.workspace === 'personal' ? this.selectedProject.project : '';
    await this.open('personal', key ? agentsPlace(key, project, origin) : kept && isAgentsPlace(kept) ? scopedAgentsPlace(kept, project) : agentsPlace('', project));
    this.universal = true;
    this.page = 'agents';
    void this.refreshOverview();
  }
  /** Agents is showing the personal frame rather than the page that lists what workspaces report. */
  get agentsInFrame(): boolean {
    return this.page === 'agents' && this.personalReady;
  }
  /** Opens Salie, on the conversation that started a session when one did. Nothing running is touched either way. */
  async openSalie(conversation?: string): Promise<void> {
    const r = this.assistantButton?.getBoundingClientRect();
    const rect = r ? { x: r.left, y: r.top, width: r.width, height: r.height } : { x: 24, y: 24, width: 0, height: 0 };
    if (!this.assistantShown) await this.toggleAssistant(rect);
    if (this.salieReturn) this.salieBack = true;
    if (conversation && this.assistantAsk?.action === 'open') this.assistantAsk = { ...this.assistantAsk, conversation };
    else if (conversation && this.assistantShown) this.assistantAsk = { action: 'open', conversation, rect };
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

  /** Opens Salie with a prompt written for her. With `sendNow` the person has already asked for the answer (pressed a button for it), so it is sent. */
  async explainCatchUp(prompt: string, sendNow = false): Promise<void> {
    await this.toggleAssistant({ x: 24, y: 24, width: 0, height: 0 });
    if (this.assistantAsk) this.assistantAsk = { action: 'open', rect: this.assistantAsk.rect, catchUpPrompt: prompt.slice(0, 24000), ...(sendNow ? { sendNow: true } : {}) };
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
    if (was) queueMicrotask(() => this.assistantButton?.focus({ preventScroll: true }));
    // Salie was brought back from a session she had opened and is now put away: the window goes back to where she was first opened.
    if (was && this.salieReturn && this.salieBack && this.page === 'agents') {
      const back = this.salieReturn;
      this.salieReturn = null;
      this.salieBack = false;
      this.restore(back);
    }
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

  /** The origin the window's own page the person is on gives to a detail page opened from it. */
  private get here(): Origin | undefined {
    return this.page === 'inbox' ? { kind: 'work' } : this.page === 'calendar' ? { kind: 'calendar' } : this.page === 'agents' ? { kind: 'agents' } : undefined;
  }

  /**
   * A task, a plan or a session of the person's own Werkbord, opened from one of the window's pages, remembers that page, so that Back
   * on it returns there (and a reload or a pasted address still knows). A place in a Team workspace cannot carry that in its address,
   * so the window remembers it and offers the way back itself (ReturnBar). Anything else is opened as it is.
   */
  private stamped(id: string, place: string | undefined): string | undefined {
    const here = this.here;
    if (!here) {
      // Moving about inside a workspace is not leaving one of the window's pages, but a Team ticket opened from another workspace's page
      // (a Salie card, the palette) has nowhere else to go Back to: the bar names the place it was opened from.
      this.departed = null;
      if (id !== 'personal' && place?.startsWith('?') && /[?&]ticket=/.test(place) && this.page === 'workspace' && this.current !== id) {
        const name = this.view?.items.find((i) => i.id === this.current)?.name ?? 'Back';
        this.departed = { page: 'workspace', context: this.selectedProject, workspace: id, place, from: { workspace: this.current, place: this.places[this.current], label: name } };
      }
    }
    if (!place || !here) return place;
    if (id !== 'personal') {
      if (place.startsWith('?')) this.departed = { page: this.page, context: this.selectedProject, workspace: id, place };
      return place;
    }
    if (!/^#\/p\/[^/?]+\/task\/[^/?]+/.test(place) && !/^#\/p\/[^/?]+\/work\?(?:.*&)?plan=/.test(place)) return place;
    if (chainOf(place).length) return place;
    this.departed = { page: this.page, context: this.selectedProject, workspace: id, place };
    return withChain(place, pushed(here, []));
  }

  /** Back to one of the window's own pages from a detail page: the page as it was left, with the project context it had. */
  returnTo(page: 'inbox' | 'calendar' | 'agents'): void {
    if (this.departed?.page === page) {
      this.selectedProject = this.departed.context;
      try { localStorage.setItem('werkbord.shell.project', contextKey(this.selectedProject)); } catch { /* Optional persistence. */ }
    }
    this.departed = null;
    this.go(page);
  }

  /**
   * Shows a place in a workspace, whichever one is open now: the sidebar's way of reaching a project or a team's people. A workspace
   * that already has a page keeps it (nothing in it restarts); otherwise it is opened.
   */
  async goTo(id: string, place?: string): Promise<void> {
    place = this.stamped(id, place);
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
    else await this.restoreView();
    this.viewRestored = true;
    void this.offerTeamUpdate();
  }

  private static readonly TEAM_UPDATE_ASKED = 'werkbord.shell.team-update-asked';

  /**
   * After Werkbord itself is updated (in the app or by installing a newer copy), Team's background service is still the earlier
   * version until it is replaced, which needs an administrator's password. Rather than leave the teams unavailable until the person
   * finds Update Team, ask once as the window opens. Declining is remembered for this window; opening Werkbord again asks again, and
   * Devices & Hosting always has the button.
   */
  async offerTeamUpdate(): Promise<void> {
    if (this.view?.team.state !== 'outdated' || !this.view.teamInstaller || this.busy) return;
    try {
      if (sessionStorage.getItem(Model.TEAM_UPDATE_ASKED)) return;
      sessionStorage.setItem(Model.TEAM_UPDATE_ASKED, '1');
    } catch { /* Not remembered: the worst case is being asked again after a reload. */ }
    this.busy = 'Updating Team…';
    try {
      await ask((a) => a.ActivateTeam());
      this.notice = 'Team is updated and your teams are back.';
    } catch (e) {
      // "Not now" is an answer, not a failure. Anything else the installer said is shown as it is.
      const message = (e as Error).message;
      if (message !== 'Team was not set up') this.error = message;
    } finally {
      this.busy = '';
      await this.refresh();
      void this.refreshOverview();
    }
  }

  /**
   * Where the window was, kept for the length of the window (a reload keeps it, a closed window starts again on Work): the page, the
   * workspace page and place on show, and the pane beside Work's list. Team pages are not addressed by the browser, so this is what
   * lets a reload come back to the ticket that was open.
   */
  private static readonly VIEW_KEY = 'werkbord.shell.view';
  private viewRestored = $state(false);

  saveView(): void {
    if (!this.viewRestored || !this.loaded) return;
    const state = { page: this.page, current: this.current, place: this.places[this.current] ?? null, pane: this.pane ? { workspace: this.pane.workspace, place: this.pane.place } : null };
    try { sessionStorage.setItem(Model.VIEW_KEY, JSON.stringify(state)); } catch { /* Not remembered; nothing is lost but the place. */ }
  }

  private async restoreView(): Promise<void> {
    type Saved = { page?: unknown; current?: unknown; place?: unknown; pane?: { workspace?: unknown; place?: unknown } | null } | null;
    let saved: Saved;
    try { saved = JSON.parse(sessionStorage.getItem(Model.VIEW_KEY) ?? 'null') as Saved; } catch { return; }
    if (!saved || typeof saved !== 'object') return;
    const placeOk = (v: unknown): v is string => typeof v === 'string' && v.length <= 2000 && (v.startsWith('#/') || v.startsWith('?'));
    const known = (id: unknown): id is string => typeof id === 'string' && !!this.view?.items.some((i) => i.id === id && i.state !== 'unavailable');
    if (saved.page === 'workspace') {
      if (known(saved.current) && placeOk(saved.place)) await this.goTo(saved.current, saved.place);
    } else if (saved.page === 'calendar' || saved.page === 'agents' || saved.page === 'reviews' || saved.page === 'analytics') {
      this.go(saved.page);
    } else if (saved.page !== 'inbox') return;
    const pane = saved.pane;
    if (pane && known(pane.workspace) && placeOk(pane.place) && saved.page === 'inbox') await this.openPane(pane.workspace, pane.place);
  }

  stop(): void {
    this.timers.forEach(clearInterval);
    this.timers = [];
  }

  private overviewMisses = 0;
  private unreachableLooks: Record<string, number> = {};

  /**
   * A workspace that was ready and is reported connecting or offline for a look or two is still shown as ready: the rows of a workspace
   * that is not ready are withdrawn, and a blip would blank them. Every other change (leaving, set up, unavailable) applies at once.
   */
  private steady(view: WorkspaceView, previous: WorkspaceView | null): WorkspaceView {
    const items = view.items.map((item) => {
      const was = previous?.items.find((i) => i.id === item.id);
      if (item.state === 'ready') { delete this.unreachableLooks[item.id]; return item; }
      if ((item.state === 'connecting' || item.state === 'offline') && was?.state === 'ready') {
        const looks = (this.unreachableLooks[item.id] = (this.unreachableLooks[item.id] ?? 0) + 1);
        if (looks < LOOKS_BEFORE_UNREACHABLE) return { ...item, state: 'ready' as const, detail: was.detail };
      }
      return item;
    });
    return { ...view, items };
  }

  async refresh(): Promise<void> {
    try {
      const fresh = await ask((a) => a.Workspaces());
      const previous = this.view;
      const view = this.steady(fresh, previous);
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
      this.overviewMisses = 0;
      // Membership may disappear while reads are in flight. Do not restore a retired workspace's old rows.
      this.overview = { ...overview, entries: this.accessibleEntries(overview) };
      decisions.sync(this.overview);
      this.notePushes(this.overview);
      const selected = this.selectedProject;
      const source = selected && this.overview.entries.find(e => e.workspace.id === selected.workspace);
      if (selected && source?.summary && !source.error && this.view?.items.find(i => i.id === selected.workspace)?.state === 'ready' && !source.summary.projects.some(p => p.id === selected.project)) {
        this.selectProject(null);
        this.notice = 'The selected project is no longer accessible. Showing All Projects.';
      }
      return true;
    } catch {
      // One missed answer is a hiccup, not a loss of access: the rows stay as they were (the window says it is catching up) rather than
      // vanishing and coming back ten seconds later. Only a run of misses withdraws them, below.
      this.overviewMisses++;
      if (this.overview && this.overviewMisses < MISSES_BEFORE_WITHDRAWN) return false;
      // Keep project labels for recovery, but never present old records as a current authorised aggregate.
      const previous = this.overview ?? { at: '', entries: (this.view?.items ?? []).map(workspace => ({ workspace })) };
      this.overview = { ...previous, entries: previous.entries.map(e => ({ ...e, error: 'Workspace summaries could not be refreshed. Reconnect to try again.' })) };
      decisions.sync(this.overview);
      return false;
    }
  }

  private accessibleEntries(overview: Overview): Overview['entries'] {
    return overview.entries.filter(e => this.view?.items.some(i => i.id === e.workspace.id && i.state !== 'leaving' && i.state !== 'setup' && i.state !== 'unavailable'));
  }

  /** Opens a workspace on the Workspace page, and optionally somewhere inside it. */
  async open(id: string, place?: string): Promise<void> {
    if (this.departed && this.departed.workspace !== id) this.departed = null;
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
    if (!hosted && this.queuedReports[id]?.length && this.flushReports) {
      const send = this.flushReports;
      const queued = this.queuedReports[id];
      delete this.queuedReports[id];
      queueMicrotask(() => queued.forEach(m => send(id, m)));
    }
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

  /**
   * Where the person was when they opened Settings or Help, so that leaving them goes back to that very place (the open task, the pane
   * beside Work's list), not to the top of a page. Kept only while Settings or Help is on show.
   */
  settingsReturn = $state<{ page: Page; current: string; place: string | undefined; pane: { workspace: string; place: string } | null } | null>(null);

  /** Where a Salie that opened a session came from, so that closing Salie again, after coming back to it, lands there. */
  private salieReturn: { page: Page; current: string; place: string | undefined; pane: { workspace: string; place: string } | null } | null = null;
  private salieBack = false;

  private snapshot() {
    return { page: this.page, current: this.current, place: this.places[this.current], pane: this.pane ? { ...this.pane } : null };
  }

  private restore(back: NonNullable<Model['settingsReturn']>): void {
    if (back.page === 'workspace') {
      this.page = 'workspace';
      this.current = back.current;
      void this.goTo(back.current, back.place);
      return;
    }
    this.go(back.page);
    if (back.pane) void this.openPane(back.pane.workspace, back.pane.place);
  }

  private rememberBeforeSettings(): void {
    if (this.page === 'settings' || this.page === 'help') return;
    this.settingsReturn = { page: this.page, current: this.current, place: this.places[this.current], pane: this.pane ? { ...this.pane } : null };
  }

  /** Leaves Settings or Help for where they were opened from. Without that, Work. */
  leaveSettings(): void {
    const back = this.settingsReturn;
    this.settingsReturn = null;
    if (!back || back.page === 'settings' || back.page === 'help') return this.go('inbox');
    this.restore(back);
  }

  /** Settings, at a section if one is asked for. Whichever part of Werkbord the person is in, it is the same screen. */
  openSettings(section?: string): void {
    this.rememberBeforeSettings();
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
    this.rememberBeforeSettings();
    this.leaveAssistant();
    this.page = 'help';
    void this.host('personal', '#/help');
  }

  /** Where a project's Git is in its workspace's own page: the person's own Werkbord has a page for it, a team has its repository tab. */
  private gitPlace(p: ProjectIdentity, reported = false): string {
    const live = !reported && p.workspace !== 'personal' && this.personalReady ? this.githubRepoOf(p) : '';
    if (live) return `#/github/${live.split('/').map(encodeURIComponent).join('/')}`;
    return p.workspace === 'personal' ? `#/p/${encodeURIComponent(p.project)}/git` : `?tab=repository&project=${encodeURIComponent(p.project)}`;
  }
  /** The team repositories on GitHub that the person's own Werkbord watches for pushes, once each. */
  get watchedRepos(): string[] {
    const out = new SvelteSet<string>();
    for (const p of this.projects) if (p.workspace !== 'personal') { const r = this.githubRepoOf(p); if (r) out.add(r); }
    return [...out].sort();
  }
  /** Teams whose pushes the person does not want to be told of. The card still lists them. */
  mutedTeams = $state<string[]>((() => { try { return JSON.parse(localStorage.getItem('werkbord.shell.repo-mute') ?? '[]') as string[]; } catch { return []; } })());
  setTeamMuted(workspace: string, muted: boolean): void {
    this.mutedTeams = muted ? [...new SvelteSet([...this.mutedTeams, workspace])] : this.mutedTeams.filter(w => w !== workspace);
    try { localStorage.setItem('werkbord.shell.repo-mute', JSON.stringify(this.mutedTeams)); } catch { /* Kept for this window only. */ }
  }
  private lastHeads: Record<string, { fingerprint: string; at: number }> = {};
  private pushesSeen: Set<string> | null = null;
  /**
   * Pushes a team reports, whoever saw them first: told here to a member who has no GitHub, or whose own Werkbord has not yet. The
   * first look only learns what is already there, so opening the window is not a burst of old news.
   */
  private notePushes(overview: Overview): void {
    const fresh: { workspace: string; push: NonNullable<NonNullable<Overview['entries'][number]['summary']>['pushes']>[number] }[] = [];
    const first = this.pushesSeen === null;
    const seen = this.pushesSeen ?? new SvelteSet<string>();
    for (const e of overview.entries) for (const push of e.summary?.pushes ?? []) {
      if (seen.has(push.key)) continue;
      seen.add(push.key);
      fresh.push({ workspace: e.workspace.id, push });
    }
    this.pushesSeen = seen;
    for (const { workspace, push } of fresh.reverse()) {
      const project = projectInHref('team', push.href);
      const found = project ? this.projects.find(p => p.workspace === workspace && p.project === project) : undefined;
      const context = found ? contextKey(found) : '';
      // The person's own Werkbord may have told it already, from GitHub itself.
      if (this.repoChanges.some(c => c.context === context && c.branch === push.branch && (c.sha.startsWith(push.sha) || push.sha.startsWith(c.sha)))) continue;
      this.repoChanges = [{ repo: '', branch: push.branch, sha: push.sha, at: Date.parse(push.at) || Date.now(), project: found?.name ?? push.project, context, workspace }, ...this.repoChanges].slice(0, 20);
      if (!first && !push.mine && !this.mutedTeams.includes(workspace)) void alerts.tell({ key: `team-push:${push.key}`, title: found?.name ?? push.project, body: `${push.branch} was updated` });
    }
  }
  private queuedReports: Record<string, Record<string, unknown>[]> = {};
  /**
   * Tells each team that has a project on this repository where its branches are on GitHub, as the person's own Werkbord just saw
   * them, so the team can tell every member (also those without GitHub) when one moves. A team's page that is not open yet is
   * opened out of sight for it. Only what changed since the last time is sent.
   */
  passHeadsToTeams(repo: string, heads: { name: string; sha: string }[], send: (workspace: string, message: Record<string, unknown>) => void): void {
    const fingerprint = JSON.stringify(heads);
    // Told again after a few minutes even if nothing moved: a team's page that was not signed in yet drops what it is sent.
    const last = this.lastHeads[repo];
    if (last?.fingerprint === fingerprint && Date.now() - last.at < 5 * 60_000) return;
    this.lastHeads[repo] = { fingerprint, at: Date.now() };
    const branches = heads.map(h => ({ name: h.name, sha: h.sha }));
    for (const p of this.projects) {
      if (p.workspace === 'personal' || this.githubRepoOf(p) !== repo) continue;
      const message = { type: 'werkbord.reportbranches', project: p.project, branches };
      const frame = this.frames[p.workspace];
      if (frame?.ready) { send(p.workspace, message); continue; }
      this.queuedReports[p.workspace] = [...(this.queuedReports[p.workspace] ?? []).filter(m => m.project !== p.project), message];
      if (!frame) void ask(a => a.LoadWorkspace(p.workspace)).then(target => { if (!this.frames[p.workspace]) this.frames[p.workspace] = { target, ready: false }; }).catch(() => { /* Told again at the next look. */ });
    }
    this.flushReports = send;
  }
  private flushReports: ((workspace: string, message: Record<string, unknown>) => void) | null = null;

  /** Recent pushes to those repositories, newest first. What the Overview shows. */
  repoChanges = $state<(RepoChange & { at: number; project: string; context: string; workspace: string })[]>([]);
  noteRepoChange(c: RepoChange): string {
    const found = this.projects.find(p => p.workspace !== 'personal' && this.githubRepoOf(p) === c.repo);
    const project = found?.name ?? c.repo;
    this.repoChanges = [{ ...c, at: Date.now(), project, context: found ? contextKey(found) : '', workspace: found?.workspace ?? '' }, ...this.repoChanges.filter(x => !(x.repo === c.repo && x.branch === c.branch && x.sha === c.sha))].slice(0, 20);
    return project;
  }

  /** owner/name of a team project's GitHub repository, from the address its workspace reports; '' when it has none, or not on github.com. */
  private githubRepoOf(p: ProjectIdentity): string {
    const entry = this.overview?.entries.find(e => e.workspace.id === p.workspace)?.summary?.projects.find(x => x.id === p.project);
    if (entry?.liveOff) return '';
    const address = entry?.repository ?? '';
    const m = /^https:\/\/([A-Za-z0-9.-]+)\/([A-Za-z0-9][A-Za-z0-9._-]*)\/([A-Za-z0-9._][A-Za-z0-9._-]*?)(?:\.git)?\/?$/.exec(address.trim());
    // github.com, or a company's GitHub host (one with "github" in its name; the person's gh must be signed in to it). Any other host is not asked.
    if (m && m[1] !== 'github.com' && !m[1].toLowerCase().includes('github')) return '';
    return m ? (m[1] === 'github.com' ? `${m[2]}/${m[3]}` : `${m[1]}/${m[2]}/${m[3]}`) : '';
  }
  private workPlace(p: ProjectIdentity): string {
    return p.workspace === 'personal' ? `#/p/${encodeURIComponent(p.project)}/work` : `?tab=work&project=${encodeURIComponent(p.project)}`;
  }
  private onTab(personal: RegExp, team: string): boolean {
    if (this.page !== 'workspace') return false;
    const place = this.places[this.current] ?? '';
    return this.current === 'personal' ? personal.test(place) : place.startsWith('?') && new SvelteURLSearchParams(place.slice(1)).get('tab') === team && !!new SvelteURLSearchParams(place.slice(1)).get('project');
  }
  /** A workspace's page is showing a project's Git. */
  get onGit(): boolean {
    if (this.page === 'workspace' && this.current === 'personal' && /^#\/github\//.test(this.places.personal ?? '')) return true;
    return this.onTab(/^#\/p\/[^/?#]+\/git(?:[/?#]|$)/, 'repository');
  }
  /** A workspace's page is showing a project's Work (its queue and plans). */
  get onProjectWork(): boolean {
    return this.onTab(/^#\/p\/[^/?#]+\/work(?:[/?#]|$)/, 'work');
  }

  /**
   * The queue and plans of the selected project. That page is the one that saves a new order, so it is shown as it is; its Overview
   * tab brings the window's Overview back.
   */
  openProjectWork(): void {
    const sel = this.selectedProject;
    if (sel) void this.goTo(sel.workspace, this.workPlace(sel));
  }

  /** Back to the window's Work, on its Overview. */
  showOverview(): void {
    viewState.work = { ...viewState.work, section: 'overview' };
    this.go('inbox');
  }

  /** The team's own report of a project's repository, for a member who cannot see it on GitHub. */
  async openReportedGit(): Promise<void> {
    const sel = this.selectedProject;
    if (sel) await this.goTo(sel.workspace, this.gitPlace(sel, true));
  }

  /**
   * Git: the selected project's own, in the page of the workspace it belongs to. With every project selected there is no one
   * repository to show, so the window lists them.
   */
  async openGit(): Promise<void> {
    const sel = this.selectedProject;
    this.salieReturn = null;
    this.departed = null;
    this.pane = null;
    if (sel) {
      await this.goTo(sel.workspace, this.gitPlace(sel));
      return;
    }
    this.universal = true;
    this.leaveAssistant();
    this.page = 'git';
    void this.refreshOverview();
  }

  /** Shows a page of the shell's own. */
  go(p: Page): void {
    this.salieReturn = null;
    this.salieBack = false;
    if (p === 'settings' || p === 'help' || p === 'workspaces') this.rememberBeforeSettings();
    else if (this.page === 'settings' || this.page === 'help') this.settingsReturn = null;
    this.departed = null;
    this.pane = null;
    if (p === 'settings') return this.openSettings();
    if (p === 'help') return this.openHelp();
    if (p === 'workspaces') {
      void this.refreshOverview();
      return this.openSettings('general/workspaces');
    }
    if (p === 'git') return void this.openGit();
    if (p === 'agents' && this.personalReady) return void this.openAgents();
    if (['calendar', 'inbox', 'reviews', 'analytics', 'agents'].includes(p)) this.universal = true;
    this.leaveAssistant();
    this.page = p;
    if (p === 'calendar' || p === 'inbox' || p === 'reviews' || p === 'analytics' || p === 'agents') void this.refreshOverview();
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
