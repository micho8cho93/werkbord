// The router: the current page, kept in step with the address bar. Hash routing
// means the shell works from any static host and from the service worker cache.

import { hrefOf, parse, queryOf, type Location, type View } from './location';

export * from './location';

class Router {
  view = $state<View>(parse(location.hash).view);
  /** The project the address names. Empty on a global page, and on an old link that names none. */
  projectId = $state<string>(parse(location.hash).projectId);
  taskId = $state<string>(parse(location.hash).taskId);
  /** Git's drill-down, below `git/`; empty everywhere else. */
  sub = $state<string>(parse(location.hash).sub ?? '');
  /** What follows `?`: a page's own state (the Settings section shown). */
  query = $state<string>(queryOf(location.hash));

  constructor() {
    window.addEventListener('hashchange', () => this.read());
  }

  private read() {
    const loc = parse(location.hash);
    this.view = loc.view;
    this.projectId = loc.projectId;
    this.taskId = loc.taskId;
    this.sub = loc.sub ?? '';
    this.query = queryOf(location.hash);
  }

  get location(): Location {
    return this.sub
      ? { view: this.view, projectId: this.projectId, taskId: this.taskId, sub: this.sub }
      : { view: this.view, projectId: this.projectId, taskId: this.taskId };
  }

  /** Goes to a page. Adds a history entry, unless `replace` is set, which is for settling an address. */
  go(loc: Location, replace = false): void {
    const href = hrefOf(loc);
    if (location.hash === href) return;
    if (replace) {
      history.replaceState(null, '', location.pathname + location.search + href);
      // Settling an address raises no hashchange; the desktop window, which marks where this page is, is told this way.
      window.dispatchEvent(new Event('werkbord:place'));
    } else location.hash = href;
    this.read();
  }
}

export const router = new Router();
