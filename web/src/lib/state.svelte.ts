// Application state shared by all views. The controller is the source of
// truth: this module holds a cache that is filled over HTTP and kept current
// by the event stream.

import { ApiError, api, eventsURL } from './api';
import type { ControllerEvent, Project, Task } from './types';

export type Connection = 'connecting' | 'live' | 'offline' | 'unauthorized';

const SELECTED_KEY = 'devboard.project';

function loadSelected(): string {
  try {
    return localStorage.getItem(SELECTED_KEY) ?? '';
  } catch {
    return '';
  }
}

class AppState {
  connection = $state<Connection>('connecting');
  projects = $state<Project[]>([]);
  selectedProjectId = $state<string>(loadSelected());
  tasks = $state<Task[]>([]);
  error = $state<string>('');

  /** Bumped on every event so views can refetch what they show. */
  revision = $state(0);

  selectedProject = $derived(this.projects.find((p) => p.id === this.selectedProjectId));

  private source: EventSource | null = null;

  start(): void {
    this.connect();
  }

  selectProject(id: string): void {
    this.selectedProjectId = id;
    try {
      localStorage.setItem(SELECTED_KEY, id);
    } catch {
      // Not persisted in private mode.
    }
    void this.loadTasks();
  }

  async refresh(): Promise<void> {
    try {
      this.projects = await api.listProjects();
      if (!this.projects.some((p) => p.id === this.selectedProjectId)) {
        this.selectedProjectId = this.projects[0]?.id ?? '';
      }
      await this.loadTasks();
      this.error = '';
    } catch (err) {
      this.handleError(err);
    }
  }

  async loadTasks(): Promise<void> {
    const id = this.selectedProjectId;
    if (!id) {
      this.tasks = [];
      return;
    }
    try {
      const tasks = await api.listTasks(id);
      if (id === this.selectedProjectId) this.tasks = tasks;
    } catch (err) {
      this.handleError(err);
    }
  }

  /** Replace or insert a task returned by a mutation, without waiting for its event. */
  upsertTask(task: Task): void {
    if (task.projectId !== this.selectedProjectId) return;
    const i = this.tasks.findIndex((t) => t.id === task.id);
    if (i === -1) this.tasks.push(task);
    else if (this.tasks[i].version <= task.version) this.tasks[i] = task;
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
      void this.refresh();
    };
    es.onerror = () => {
      this.connection = 'offline';
      // While CONNECTING the browser retries by itself. CLOSED means it gave
      // up after an HTTP error, which is usually a missing token.
      if (es.readyState === EventSource.CLOSED) void this.diagnose();
    };
    const onEvent = (msg: MessageEvent<string>) => this.apply(JSON.parse(msg.data) as ControllerEvent);
    for (const type of [
      'project.registered',
      'project.inspected',
      'task.created',
      'task.updated',
      'run.state_changed',
      'question.created',
      'question.answered',
    ]) {
      es.addEventListener(type, onEvent);
    }
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
    this.revision++;
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
    }
  }
}

export const app = new AppState();
