// Application state shared by all views. The controller is the source of
// truth: this module holds a cache that is filled over HTTP and kept current
// by the event stream.
//
// The cache is split the way the application is. What is global lives here: the
// connection, the projects, the agents installed on this computer and the Control
// Center's overview. What belongs to a project lives in that project's
// ProjectScope (see scope.svelte.ts), one per project visited, and is never
// mixed with another's.

import { SvelteMap } from 'svelte/reactivity';
import { ApiError, api, eventsURL } from './api';
import { gitEvent } from './git/store.svelte';
import { QuestionBook } from './questions';
import { ProjectScope } from './scope.svelte';
import type { Agent, ControllerEvent, Overview, Project, Question } from './types';

export type Connection = 'connecting' | 'live' | 'offline' | 'unauthorized';

const LAST_PROJECT_KEY = 'devboard.project';

function loadLastProject(): string {
  try {
    return localStorage.getItem(LAST_PROJECT_KEY) ?? '';
  } catch {
    return '';
  }
}

/** Every event type the controller sends that the app reacts to. */
const EVENT_TYPES = [
  'project.registered',
  'project.inspected',
  'task.created',
  'task.updated',
  'run.state_changed',
  'question.answered',
  'question.cancelled',
  'agent.started',
  'agent.output',
  'agent.question',
  'agent.waiting',
  'agent.blocked',
  'agent.resumed',
  'agent.completed',
  'agent.failed',
  'agent.stopped',
  'worktree.created',
  'worktree.removing',
  'worktree.removed',
  'git.fetched',
  'git.pushed',
  'git.merged',
  'git.branch_deleted',
  'git.worktree_cleaned',
  'git.pull_request_created',
];

/** Events after which the Control Center's overview may be out of date. Agent output is not one of them. */
const OVERVIEW_EVENTS = new Set([
  'project.registered',
  'task.created',
  'task.updated',
  'run.state_changed',
  'agent.question',
  'agent.blocked',
  'question.answered',
  'question.cancelled',
]);

type RunListener = (ev: ControllerEvent) => void;

/** Where something belongs, for showing it outside its project. */
export interface TaskInfo {
  title: string;
  projectId: string;
  projectName: string;
}

class AppState {
  connection = $state<Connection>('connecting');
  projects = $state<Project[]>([]);
  agents = $state<Agent[]>([]);
  /** What needs the user, in every project. Null until first fetched. */
  overview = $state<Overview | null>(null);
  /** Questions still waiting for the user, in every project, oldest first. Kept by `book`. */
  questions = $state<Question[]>([]);
  error = $state<string>('');
  /** A message that outlives whatever raised it, such as "already answered on another device". */
  notice = $state<string>('');
  /** The project last visited: where a link that names none goes, and what the tabs point at on a global page. */
  lastProjectId = $state<string>(loadLastProject());
  /** Whether the project switcher is open. */
  switcherOpen = $state(false);

  /** The time, updated every second while the page is visible, for elapsed times. */
  now = $state(Date.now());

  /** One scope per project visited. */
  readonly scopes = new SvelteMap<string, ProjectScope>();

  /** Runs that are blocked, anywhere: waiting for the user to settle something. */
  blockedCount = $derived((this.overview?.runs ?? []).filter((r) => r.run.state === 'blocked').length);
  /** Runs that finished a turn and wait for the next message, anywhere. */
  idleCount = $derived(
    (this.overview?.runs ?? []).filter((r) => r.run.state === 'waiting_for_user' && r.run.waiting === 'idle').length,
  );
  /** What waits for the user, in every project: open questions, blocked runs, runs idle for a next message. Counted for the badge. */
  needsYou = $derived(this.questions.length + this.blockedCount + this.idleCount);
  /** How many questions are waiting for an answer: what the "needs input" signals count. */
  needsInput = $derived(this.questions.length);

  private book = new QuestionBook();
  private noticeTimer: ReturnType<typeof setTimeout> | undefined;
  private overviewTimer: ReturnType<typeof setTimeout> | undefined;
  private source: EventSource | null = null;
  private listeners = new Map<string, RunListener[]>();
  private reconnectHandlers: (() => void)[] = [];

  start(): void {
    this.connect();
    setInterval(() => {
      if (document.visibilityState === 'visible') this.now = Date.now();
    }, 1000);
    document.addEventListener('visibilitychange', () => {
      this.now = Date.now();
    });
  }

  // ---- projects ----

  project(id: string): Project | undefined {
    return this.projects.find((p) => p.id === id);
  }

  /** Replaces a project returned by the controller (after a refresh), or adds it. */
  upsertProject(p: Project): void {
    const i = this.projects.findIndex((x) => x.id === p.id);
    if (i === -1) this.projects.push(p);
    else this.projects[i] = p;
  }

  /**
   * Makes a project the one being worked in: remembers it, and has its data (a scope)
   * ready, fetching it the first time. A project already visited is there at once, so
   * switching back to it needs no loading. It must not be called while rendering.
   */
  enter(projectId: string): ProjectScope | undefined {
    if (!projectId) return undefined;
    this.lastProjectId = projectId;
    try {
      localStorage.setItem(LAST_PROJECT_KEY, projectId);
    } catch {
      // Not persisted in private mode.
    }
    let scope = this.scopes.get(projectId);
    if (!scope) {
      scope = new ProjectScope(projectId);
      this.scopes.set(projectId, scope);
    }
    if (!scope.loaded && !scope.loading) void scope.load().catch((err) => this.handleError(err));
    return scope;
  }

  /**
   * Has a project's data fetched ahead of its being opened (as the switcher does for the
   * projects it lists), without making it the current project. Nothing is shown from it
   * until it is entered; it only means that switching to it finds it ready.
   */
  warm(projectId: string): void {
    if (!projectId || this.scopes.has(projectId)) return;
    const scope = new ProjectScope(projectId);
    this.scopes.set(projectId, scope);
    void scope.load().catch((err) => this.handleError(err));
  }

  /** The data of a project, if it has been entered. */
  scope(projectId: string): ProjectScope | undefined {
    return this.scopes.get(projectId);
  }

  // ---- refreshing ----

  async refresh(): Promise<void> {
    try {
      const sync = this.book.beginSync();
      const [projects, agents, overview] = await Promise.all([api.listProjects(), api.listAgents(), api.controlCenter()]);
      this.projects = projects;
      this.agents = agents;
      this.overview = overview;
      if (this.book.replace(sync, overview.questions.map((q) => q.question))) this.questions = this.book.pending;
      this.error = '';
      // Projects already open catch up on what they missed.
      await Promise.all([...this.scopes.values()].map((s) => s.load().catch((err) => this.handleError(err))));
    } catch (err) {
      this.handleError(err);
    }
  }

  async loadAgents(): Promise<void> {
    try {
      this.agents = await api.listAgents();
    } catch (err) {
      this.handleError(err);
    }
  }

  /** Fetches the Control Center's overview: soon, and once for a burst of events. */
  refreshOverview(): void {
    clearTimeout(this.overviewTimer);
    this.overviewTimer = setTimeout(() => void this.loadOverview(), 250);
  }

  private async loadOverview(): Promise<void> {
    try {
      const sync = this.book.beginSync();
      const overview = await api.controlCenter();
      this.overview = overview;
      if (this.book.replace(sync, overview.questions.map((q) => q.question))) this.questions = this.book.pending;
    } catch (err) {
      this.handleError(err);
    }
  }

  /** A question is settled (answered here or elsewhere, or closed): stop offering it, wherever it is shown, without waiting for its event. */
  resolveQuestion(q: Pick<Question, 'id' | 'projectId'>): void {
    this.book.resolve(q);
    this.questions = this.book.pending;
    this.scopes.get(q.projectId)?.resolveQuestion(q);
  }

  /** Where a task is and what it is called, for showing it outside its project, as the banner and the Control Center do. */
  taskInfo(taskId: string, projectId: string): TaskInfo | undefined {
    const project = this.project(projectId);
    const title =
      this.scopes.get(projectId)?.tasks.find((t) => t.id === taskId)?.title ??
      this.overview?.questions.find((q) => q.question.taskId === taskId)?.taskTitle ??
      this.overview?.runs.find((r) => r.run.taskId === taskId)?.taskTitle;
    if (title === undefined) return undefined;
    return { title, projectId, projectName: project?.name ?? '' };
  }

  /** Shows a message app-wide for a while. */
  notify(message: string, ms = 9000): void {
    this.notice = message;
    clearTimeout(this.noticeTimer);
    this.noticeTimer = setTimeout(() => (this.notice = ''), ms);
  }

  dismissNotice(): void {
    clearTimeout(this.noticeTimer);
    this.notice = '';
  }

  /** Calls fn for every event of one run, until the returned function is called. */
  watchRun(runId: string, fn: RunListener): () => void {
    this.listeners.set(runId, [...(this.listeners.get(runId) ?? []), fn]);
    return () => {
      const rest = (this.listeners.get(runId) ?? []).filter((f) => f !== fn);
      if (rest.length > 0) this.listeners.set(runId, rest);
      else this.listeners.delete(runId);
    };
  }

  /** Calls fn each time the event stream (re)connects: a view should catch up on what it missed. */
  onReconnect(fn: () => void): () => void {
    this.reconnectHandlers = [...this.reconnectHandlers, fn];
    return () => {
      this.reconnectHandlers = this.reconnectHandlers.filter((f) => f !== fn);
    };
  }

  handleError(err: unknown): void {
    if (err instanceof ApiError && err.status === 401) {
      this.connection = 'unauthorized';
      this.source?.close();
      return;
    }
    this.error = err instanceof Error ? err.message : String(err);
  }

  reconnect(): void {
    this.source?.close();
    this.connect();
  }

  private connect(): void {
    this.connection = 'connecting';
    const es = new EventSource(eventsURL());
    this.source = es;

    // Fires on the first connection and on every automatic reconnect. The
    // browser resumes with Last-Event-ID, but refetching keeps this simple
    // and correct even if the stream was replaced.
    es.onopen = () => {
      this.connection = 'live';
      void this.refresh().then(() => this.reconnectHandlers.forEach((fn) => fn()));
    };
    es.onerror = () => {
      this.connection = 'offline';
      // While CONNECTING the browser retries by itself. CLOSED means it gave
      // up after an HTTP error, which is usually a missing token.
      if (es.readyState === EventSource.CLOSED) void this.diagnose();
    };
    const onEvent = (msg: MessageEvent<string>) => this.apply(JSON.parse(msg.data) as ControllerEvent);
    for (const type of EVENT_TYPES) es.addEventListener(type, onEvent);
  }

  private async diagnose(): Promise<void> {
    try {
      await api.listProjects();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        this.connection = 'unauthorized';
        return;
      }
    }
    setTimeout(() => this.reconnect(), 3000);
  }

  private apply(ev: ControllerEvent): void {
    if (ev.runId) this.listeners.get(ev.runId)?.forEach((fn) => fn(ev));

    // A project's events go to that project's scope, which takes nothing else.
    if (ev.projectId) this.scopes.get(ev.projectId)?.apply(ev);
    gitEvent(ev);

    switch (ev.type) {
      case 'project.registered':
      case 'project.inspected':
        void api.listProjects().then(
          (ps) => (this.projects = ps),
          (err) => this.handleError(err),
        );
        break;
      case 'agent.question':
      case 'question.answered':
      case 'question.cancelled': {
        // Each carries the whole question, so no refetch is needed for the list; the overview is refreshed for the names.
        if (this.book.apply(ev)) this.questions = this.book.pending;
        break;
      }
    }
    if (OVERVIEW_EVENTS.has(ev.type)) this.refreshOverview();
  }
}

export const app = new AppState();
