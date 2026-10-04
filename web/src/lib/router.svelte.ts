// Minimal hash router for the three primary surfaces. Hash routing means the
// shell works from any static host and from the service worker cache.

export type Route = 'board' | 'control' | 'git';

export const ROUTES: { id: Route; label: string }[] = [
  { id: 'board', label: 'Board' },
  { id: 'control', label: 'Control Center' },
  { id: 'git', label: 'Git' },
];

function parse(hash: string): Route {
  const id = hash.replace(/^#\/?/, '').split(/[/?&]/)[0];
  return ROUTES.some((r) => r.id === id) ? (id as Route) : 'board';
}

class Router {
  current = $state<Route>(parse(location.hash));

  constructor() {
    window.addEventListener('hashchange', () => {
      this.current = parse(location.hash);
    });
  }
}

export const router = new Router();
