// A project is active, then archived, then deleted, then erased: the same three steps as a task, with the same 30 days in between. These
// are the four moves, each asking first, so Project settings and Project history do them identically.

import { api } from './api';
import { confirmAction } from './confirm.svelte';
import { app } from './state.svelte';
import { TRASH_DAYS } from './trash';
import type { Project } from './types';

export type ProjectMove = 'archive' | 'delete' | 'restore' | 'erase';

/** Asks (for what cannot simply be undone, or changes what is visible), does it, and refreshes what the window lists. Returns whether it was done. */
export async function moveProject(p: Project, move: ProjectMove): Promise<boolean> {
  const sure = move === 'restore' || await confirmAction(
    move === 'archive' ? { title: `Archive ${p.name}?`, message: 'It leaves your project list and agents stop working on it. Its tasks, memory and files are kept, and you can restore it from Project history.', confirmLabel: 'Archive project' }
    : move === 'delete' ? { title: `Delete ${p.name}?`, message: `It moves to Recently deleted in Project history and can be restored for ${TRASH_DAYS} days. After that it is erased: its tasks, runs and memory. The repository and its files stay on disk.`, confirmLabel: 'Delete project' }
    : { title: `Erase ${p.name} now?`, message: 'This cannot be undone. Its tasks, runs, plans and memory are erased from Werkbord. The repository, its branches and its files on disk are not touched.', confirmLabel: 'Erase now', danger: true });
  if (!sure) return false;
  if (move === 'erase') await api.eraseProject(p.id);
  else await api.setProjectLifecycle(p.id, move === 'archive' ? 'archived' : move === 'delete' ? 'deleted' : 'active');
  app.projects = await api.listProjects();
  app.refreshOverview();
  return true;
}
