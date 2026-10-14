import '../../../internal/uipresentation/static/planning-drag.js';
import '../../../internal/uipresentation/static/planning.css';
import { api } from './api';
import type { ProjectScope } from './scope.svelte';
import { app } from './state.svelte';

export interface DateDrop { id: string; start: string; end: string; minute?: number }

/** Listen only within the owning surface, so background calendars cannot save another view's gesture. */
export function listenDates(surface: HTMLElement, save: (drop: DateDrop) => void | Promise<void>): () => void {
  const listener = async (event: Event) => {
    const item = (event.target as HTMLElement).closest<HTMLElement>('[data-plan-item]');
    if (item?.getAttribute('aria-busy') === 'true') return;
    item?.setAttribute('aria-busy', 'true');
    try { await save((event as CustomEvent<DateDrop>).detail); }
    catch (error) { app.handleError(error); }
    finally { item?.setAttribute('aria-busy', 'false'); }
  };
  surface.addEventListener('werkbord-plan-drop', listener);
  return () => surface.removeEventListener('werkbord-plan-drop', listener);
}

export async function movePlan(scope: ProjectScope, drop: DateDrop): Promise<void> {
  const task = scope.activeTasks.find(t => t.id === drop.id);
  if (!task) return;
  try {
    scope.upsertTask(await api.editTask(task, { plan: { ...task.plan, start: drop.start, end: drop.end } }));
    app.notify(`Planned dates saved for “${task.title}”.`);
  } catch (error) {
    app.notify(`Dates could not be changed. ${error instanceof Error ? error.message : String(error)}`);
    await scope.load().catch(e => app.handleError(e));
  }
}
