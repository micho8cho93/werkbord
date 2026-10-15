// Screens that look across every project read the tasks each project's scope already holds. Opening one asks each project's scope to
// load (once; a project already visited is not fetched again), and the lists below follow the scopes as events arrive.

import type { Task } from '../types';
import { app } from '../state.svelte';

/** Starts loading every project's tasks. Call from an effect; it does nothing for a project already loaded. */
export function warmProjects(): void {
  for (const p of app.projects) app.warm(p.id);
}

/** The active tasks of every project, or of one. */
export function tasksOf(projectId = ''): Task[] {
  return app.projects.filter((p) => !projectId || p.id === projectId).flatMap((p) => app.scopes.get(p.id)?.activeTasks ?? []);
}

/** Whether every project's tasks have arrived, so a screen can say "nothing" and mean it. */
export function projectsLoaded(): boolean {
  return app.projects.every((p) => app.scopes.get(p.id)?.loaded);
}
