// The Git Control Center's data for one project. It is a cache of what the controller
// says, refetched when the repository, a task or a run changes, never edited here.
//
// The local picture and GitHub are loaded by separate calls, on purpose: GitHub being
// slow, offline or signed out must not slow down or empty the local overview.

import { ApiError, api } from '../api';
import { nowISO } from '../format';
import type { ControllerEvent, GitActionResult, GitHubState, GitOverview, RepositoryHealth } from '../types';

/** Events after which what Git shows may be out of date. */
const GIT_AFFECTING = new Set([
  'run.state_changed',
  'task.updated',
  'worktree.created',
  'worktree.removing',
  'worktree.removed',
  'project.inspected',
]);

/** Health announces its own changes; they refresh the health, not the whole overview. */
export const HEALTH_EVENT = 'git.health_changed';

export const isGitEvent = (type: string): boolean => type !== HEALTH_EVENT && (type.startsWith('git.') || GIT_AFFECTING.has(type));

class GitStore {
  readonly projectId: string;
  overview = $state<GitOverview | null>(null);
  github = $state<GitHubState | null>(null);
  loading = $state(false);
  githubLoading = $state(false);
  error = $state('');
  /** The result of the last action, shown on the overview until dismissed. */
  lastResult = $state<GitActionResult | null>(null);
  /** What needs attention in this repository. Worked out by the controller from Git metadata, never by a model. */
  health = $state<RepositoryHealth | null>(null);
  healthLoading = $state(false);
  healthError = $state('');

  private loadId = 0;
  private ghId = 0;
  private healthId = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  /** Whether a screen is looking: nothing is fetched for a project nobody has opened Git in. */
  private watchers = 0;

  constructor(projectId: string) {
    this.projectId = projectId;
  }

  /** A screen starts looking. Returns what it calls when it stops. */
  watch(): () => void {
    this.watchers++;
    if (!this.overview && !this.loading) void this.load();
    else if (this.stale) void this.load();
    if (!this.health && !this.healthLoading) void this.loadHealth();
    return () => {
      this.watchers = Math.max(0, this.watchers - 1);
    };
  }

  private stale = false;

  async load(): Promise<void> {
    const id = ++this.loadId;
    this.loading = true;
    this.stale = false;
    try {
      const o = await api.git.overview(this.projectId);
      if (id !== this.loadId) return;
      this.overview = o;
      this.error = '';
    } catch (err) {
      if (id !== this.loadId) return;
      this.error = describe(err);
    } finally {
      if (id === this.loadId) this.loading = false;
    }
  }

  async loadGitHub(): Promise<void> {
    const id = ++this.ghId;
    this.githubLoading = true;
    try {
      const s = await api.git.pullRequests(this.projectId);
      if (id === this.ghId) this.github = s;
    } catch (err) {
      if (id === this.ghId) {
        this.github = { available: false, reason: 'error', message: describe(err), pullRequests: [], fetchedAt: nowISO() };
      }
    } finally {
      if (id === this.ghId) this.githubLoading = false;
    }
  }

  /**
   * What the controller last worked out. Cheap: it reads what was stored, and only looks at Git
   * again if something that can change the answer happened, or the last look is a few minutes old.
   */
  async loadHealth(): Promise<void> {
    await this.fetchHealth(() => api.git.health(this.projectId));
  }

  /** Looks at the repository again now. Git metadata only: no network, nothing changed. */
  async refreshHealth(): Promise<void> {
    await this.fetchHealth(() => api.git.refreshHealth(this.projectId));
  }

  /** The user says they know about a finding: hidden until it gets worse. */
  async dismissFinding(id: string): Promise<void> {
    await this.fetchHealth(() => api.git.dismissFinding(this.projectId, id));
  }

  async reopenFinding(id: string): Promise<void> {
    await this.fetchHealth(() => api.git.reopenFinding(this.projectId, id));
  }

  private async fetchHealth(call: () => Promise<RepositoryHealth>): Promise<void> {
    const id = ++this.healthId;
    this.healthLoading = true;
    try {
      const h = await call();
      if (id !== this.healthId) return;
      this.health = h;
      this.healthError = '';
    } catch (err) {
      if (id !== this.healthId) return;
      this.healthError = describe(err);
    } finally {
      if (id === this.healthId) this.healthLoading = false;
    }
  }

  /** Everything, after an action or on an explicit refresh: the repository, GitHub, and its health. */
  async reloadAll(): Promise<void> {
    await Promise.all([this.load(), this.loadGitHub(), this.refreshHealth()]);
  }

  /** The controller says health changed (a finding opened, resolved or got worse): read it again. */
  healthChanged(): void {
    if (this.watchers === 0) {
      this.health = null; // read afresh the next time someone looks
      return;
    }
    void this.loadHealth();
  }

  /** The repository, a task or a run changed. Refetch soon, once for a burst, and only if someone is looking. */
  touched(): void {
    this.stale = true;
    if (this.watchers === 0 || !this.overview) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.load(), 400);
  }
}

function describe(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return err instanceof Error ? err.message : String(err);
}

// A plain Map on purpose: this is a cache of objects, not state anything renders from, and it is
// filled while a component is deriving (a reactive map must not be written there).
// eslint-disable-next-line svelte/prefer-svelte-reactivity
const stores = new Map<string, GitStore>();

/** The Git data of a project, one object per project and never shared between them. */
export function gitStore(projectId: string): GitStore {
  let s = stores.get(projectId);
  if (!s) {
    s = new GitStore(projectId);
    stores.set(projectId, s);
  }
  return s;
}

/** Called for every event of the stream: the project's Git data is refetched if the event can have changed it. */
export function gitEvent(ev: ControllerEvent): void {
  if (!ev.projectId) return;
  if (ev.type === HEALTH_EVENT) stores.get(ev.projectId)?.healthChanged();
  else if (isGitEvent(ev.type)) stores.get(ev.projectId)?.touched();
}

export type { GitStore };
