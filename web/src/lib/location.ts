// Which page the address bar names. Pure, so it can be tested on its own.
//
// Three primary surfaces (Board, Control Center, Git) and one page that belongs
// to the Board: a task, at #/task/<id>.

export type Route = 'board' | 'control' | 'git';

export const ROUTES: { id: Route; label: string }[] = [
  { id: 'board', label: 'Board' },
  { id: 'control', label: 'Control Center' },
  { id: 'git', label: 'Git' },
];

export interface Location {
  route: Route;
  /** Set on a task's page. */
  taskId: string;
}

export function parse(hash: string): Location {
  const [first = '', second = ''] = hash.replace(/^#\/?/, '').split(/[?&]/)[0].split('/');
  if (first === 'task' && second) {
    try {
      return { route: 'board', taskId: decodeURIComponent(second) };
    } catch {
      return { route: 'board', taskId: '' };
    }
  }
  const route = ROUTES.find((r) => r.id === first)?.id ?? 'board';
  return { route, taskId: '' };
}

export function taskHref(id: string): string {
  return `#/task/${encodeURIComponent(id)}`;
}
