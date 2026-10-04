// The Git Control Center's data for one project. It is a cache of what the controller
// says, refetched when the repository, a task or a run changes, never edited here.
//
// The local picture and GitHub are loaded by separate calls, on purpose: GitHub being
// slow, offline or signed out must not slow down or empty the local overview.

import { ApiError, api } from '../api';
import { nowISO } from '../format';
import type { ControllerEvent, GitActionResult, GitHubState, GitOverview } from '../types';

/** Events after which what Git shows may be out of date. */
const GIT_AFFECTING = new Set([
  'run.state_changed',
  'task.updated',
  'worktree.created',
  'worktree.removing',
  'worktree.removed',
  'project.inspected',
]);

export const isGitEvent = (type: string): boolean => type.startsWith('git.') || GIT_AFFECTING.has(type);

class GitStore {
  readonly projectId: string;
  overview = $state<GitOverview | null>(null);
  github = $state<GitHubState | null>(null);
  loading = $state(false);
  githubLoading = $state(false);
  error = $state('');
  /** The result of the last action, shown on the overview until dismissed. */
  lastResult = $state<GitActionResult | null>(null);

  private loadId = 0;
  private ghId = 0;
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

  /** Both, after an action or on request. */
  async reloadAll(): Promise<void> {
    await Promise.all([this.load(), this.loadGitHub()]);
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
  if (ev.projectId && isGitEvent(ev.type)) stores.get(ev.projectId)?.touched();
}

export type { GitStore };
