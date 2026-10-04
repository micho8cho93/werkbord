// The router: the current page, kept in step with the address bar. Hash routing
// means the shell works from any static host and from the service worker cache.

import { parse, type Route } from './location';

export { ROUTES, taskHref, type Route } from './location';

class Router {
  current = $state<Route>(parse(location.hash).route);
  taskId = $state<string>(parse(location.hash).taskId);

  constructor() {
    window.addEventListener('hashchange', () => {
      const loc = parse(location.hash);
      this.current = loc.route;
      this.taskId = loc.taskId;
    });
  }
}

export const router = new Router();
