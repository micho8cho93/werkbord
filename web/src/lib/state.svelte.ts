// Application state shared by all views. The controller is the source of
// truth: this module holds a cache that is filled over HTTP and kept current
// by the event stream.

import { ApiError, api, eventsURL } from './api';
import { oneLine } from './format';
import type { Agent, AgentOutput, ControllerEvent, Project, Question, Run, Task } from './types';

export type Connection = 'connecting' | 'live' | 'offline' | 'unauthorized';

const SELECTED_KEY = 'devboard.project';

function loadSelected(): string {
  try {
    return localStorage.getItem(SELECTED_KEY) ?? '';
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
  'agent.started',
  'agent.output',
  'agent.question',
  'agent.waiting',
  'agent.resumed',
  'agent.completed',
  'agent.failed',
  'agent.stopped',
];

type RunListener = (ev: ControllerEvent) => void;

class AppState {
  connection = $state<Connection>('connecting');
  projects = $state<Project[]>([]);
  selectedProjectId = $state<string>(loadSelected());
  /** Tasks of every project: the control center names the work of all of them. */
  allTasks = $state<Task[]>([]);
  /** The most recent run of each task, by task ID, across projects. */
  latestRun = $state<Record<string, Run>>({});
  questions = $state<Question[]>([]);
  agents = $state<Agent[]>([]);
  error = $state<string>('');

  /** The time, updated every second while the page is visible, for elapsed times. */
  now = $state(Date.now());

  tasks = $derived(this.allTasks.filter((t) => t.projectId === this.selectedProjectId));
  selectedProject = $derived(this.projects.find((p) => p.id === this.selectedProjectId));
  /** Runs that still have a session: working, or waiting for the user. */
  activeRuns = $derived(
    Object.values(this.latestRun)
      .filter((r) => r.state === 'starting' || r.state === 'running' || r.state === 'waiting_for_user')
      .sort((a, b) => a.createdAt.localeCompare(b.createdAt)),
  );
  /** How many runs are blocked on the user. */
  needsYou = $derived(this.activeRuns.filter((r) => r.state === 'waiting_for_user').length);

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

  selectProject(id: string): void {
    this.selectedProjectId = id;
    try {
      localStorage.setItem(SELECTED_KEY, id);
    } catch {
      // Not persisted in private mode.
    }
  }

  async refresh(): Promise<void> {
    try {
      const [projects, active, agents, questions] = await Promise.all([
        api.listProjects(),
        api.listActiveRuns(),
        api.listAgents(),
        api.listPendingQuestions(),
      ]);
      this.projects = projects;
      this.agents = agents;
      this.questions = questions;
      if (!projects.some((p) => p.id === this.selectedProjectId)) {
        this.selectedProjectId = projects[0]?.id ?? '';
      }
      const [tasks, runs] = await Promise.all([
        Promise.all(projects.map((p) => api.listTasks(p.id))),
        Promise.all(projects.map((p) => api.listProjectRuns(p.id))),
      ]);
      this.allTasks = tasks.flat();
      const latest: Record<string, Run> = {};
      for (const r of [...runs.flat(), ...active]) latest[r.taskId] = newer(latest[r.taskId], r);
      this.latestRun = latest;
      this.error = '';
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

  async loadQuestions(): Promise<void> {
    try {
      this.questions = await api.listPendingQuestions();
    } catch (err) {
      this.handleError(err);
    }
  }

  /** Replace or insert a task returned by a mutation, without waiting for its event. */
  upsertTask(task: Task): void {
    const i = this.allTasks.findIndex((t) => t.id === task.id);
    if (i === -1) this.allTasks.push(task);
    else if (this.allTasks[i].version <= task.version) this.allTasks[i] = task;
  }

  /** Same for a run: ignores one older than what is already known. */
  upsertRun(run: Run): void {
    this.latestRun[run.taskId] = newer(this.latestRun[run.taskId], run);
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

    switch (ev.type) {
      case 'project.registered':
      case 'project.inspected':
        void api.listProjects().then((ps) => {
          this.projects = ps;
          if (!this.selectedProjectId && ps.length) this.selectProject(ps[0].id);
        }, (err) => this.handleError(err));
        break;
      case 'task.created':
      case 'task.updated':
        this.upsertTask(ev.payload as Task);
        break;
      case 'run.state_changed':
        this.upsertRun((ev.payload as { run: Run }).run);
        break;
      case 'agent.output': {
        // The card's activity line follows what the agent does, ahead of the next state change.
        const out = ev.payload as AgentOutput;
        const run = ev.taskId ? this.latestRun[ev.taskId] : undefined;
        if (run && run.id === ev.runId && (out.stream === 'assistant' || out.stream === 'tool')) {
          run.activity = oneLine(out.text);
          run.activityAt = ev.createdAt;
        }
        break;
      }
      case 'agent.question':
      case 'question.answered':
        void this.loadQuestions();
        break;
    }
  }
}

/** The more recently created of two runs of one task; on a tie, the later version. */
function newer(a: Run | undefined, b: Run): Run {
  if (!a) return b;
  if (a.id === b.id) return a.version <= b.version ? b : a;
  return a.createdAt >= b.createdAt ? a : b;
}

export const app = new AppState();
