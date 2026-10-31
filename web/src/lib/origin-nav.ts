// Going back along an origin chain (origin.ts). The same step is a link when the place is an address of this page, so it can be
// followed, copied and middle-clicked, and an action when the place is the window's (Work, Calendar) or Salie's.

import { embedded } from './embed';
import { backToSalie } from './agent-nav';
import { globalHref, projectHref, sessionsHref, taskHref } from './location';
import { withChain, type Origin } from './origin';

/** The window's own pages a page in a frame may go back to (frames.ts allows only these). */
export type ShellPage = 'inbox' | 'calendar';

/** The address of an origin in this page, shown with the rest of the chain, or null when the place is not an address of this page.
 *  `fromTask` is the task the person is leaving: a project's Calendar opens on that task's entry, week and all. */
export function originHref(o: Origin, rest: readonly Origin[], fromTask = ''): string | null {
  switch (o.kind) {
    case 'board': return withChain(projectHref(o.projectId, 'work'), rest);
    case 'plan': return withChain(`${projectHref(o.projectId, 'work')}?plan=${encodeURIComponent(o.planId)}`, rest);
    case 'task': return withChain(taskHref(o.projectId, o.taskId), rest);
    case 'session': return withChain(sessionsHref(o.key), rest);
    case 'agents': return withChain(sessionsHref(), rest);
    case 'calendar': return o.projectId ? withChain(projectHref(o.projectId, 'calendar') + (fromTask ? `?task=${encodeURIComponent(fromTask)}` : ''), rest) : embedded() ? null : withChain(globalHref('control'), rest);
    case 'work': return embedded() ? null : withChain(globalHref('control'), rest);
    case 'salie': return null;
  }
}

/** Goes to an origin: the address when it has one, otherwise the window's page, or Salie. Nothing running is touched either way. */
export function goOrigin(o: Origin, rest: readonly Origin[], fromTask = ''): void {
  const href = originHref(o, rest, fromTask);
  if (href) { location.hash = href; return; }
  if (o.kind === 'salie') { backToSalie(o.conversation); return; }
  if (o.kind === 'work' || o.kind === 'calendar') {
    window.parent.postMessage({ type: 'werkbord.frame', event: 'page', page: (o.kind === 'work' ? 'inbox' : 'calendar') satisfies ShellPage }, '*');
  }
}
