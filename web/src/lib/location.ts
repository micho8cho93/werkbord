// Which page the address bar names, and how to name one. Pure, so it can be
// tested on its own.
//
// Project is the scope of the application. There are two kinds of page:
//
//   global          #/control          the Control Center: what needs you, in every project
//                   #/projects         every registered repository, and registering one
//   in a project    #/p/<id>/board     (also: git, activity)
//                   #/p/<id>/git/...   Git drills down: branch, file, changes (see gitroute.ts)
//                   #/p/<id>/task/<id> a task, which belongs to the board
//
// A project's own pages are listed once, in PROJECT_SECTIONS. The shell, the
// switcher and the tab bar are all built from that list, so a section added to
// it (Calendar, in V1) appears everywhere without further navigation work.

export type GlobalView = 'control' | 'projects';
export type ProjectSection = 'board' | 'git' | 'activity';
export type View = GlobalView | ProjectSection | 'task';

export const GLOBAL_VIEWS: readonly { id: GlobalView; label: string; short: string }[] = [
  { id: 'control', label: 'Control Center', short: 'Control' },
  { id: 'projects', label: 'Projects', short: 'Projects' },
];

export const PROJECT_SECTIONS: readonly { id: ProjectSection; label: string }[] = [
  { id: 'board', label: 'Board' },
  { id: 'git', label: 'Git' },
  { id: 'activity', label: 'Activity' },
];

export interface Location {
  view: View;
  /** The project the page is in; empty on a global page, and on an old link that does not say. */
  projectId: string;
  /** Set on a task's page. */
  taskId: string;
  /** Git's drill-down: whatever follows `git/` in the address, with its query. Absent on every other page. */
  sub?: string;
}

const isGlobal = (v: string): v is GlobalView => GLOBAL_VIEWS.some((g) => g.id === v);
const isSection = (v: string): v is ProjectSection => PROJECT_SECTIONS.some((s) => s.id === v);

function decode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return '';
  }
}

export function parse(hash: string): Location {
  const full = hash.replace(/^#\/?/, '');
  const parts = full.split(/[?&]/)[0].split('/');
  const [first = '', second = '', third = '', fourth = ''] = parts;

  if (isGlobal(first)) return { view: first, projectId: '', taskId: '' };

  if (first === 'p' && decode(second)) {
    const projectId = decode(second);
    if (third === 'task') {
      const taskId = decode(fourth);
      // A task page without a task is the board.
      return taskId ? { view: 'task', projectId, taskId } : { view: 'board', projectId, taskId: '' };
    }
    if (third === 'git') {
      // Git has screens of its own below it. Everything after `git/`, and the query, is theirs to read.
      const q = full.indexOf('?');
      const rest = full.split(/[?]/)[0].split('/').slice(3).join('/');
      const sub = rest + (q === -1 ? '' : full.slice(q));
      return sub ? { view: 'git', projectId, taskId: '', sub } : { view: 'git', projectId, taskId: '' };
    }
    return { view: isSection(third) ? third : 'board', projectId, taskId: '' };
  }

  // Older links named a section without a project: it is the one last used.
  if (isSection(first)) return { view: first, projectId: '', taskId: '' };
  return { view: 'board', projectId: '', taskId: '' };
}

/** Whether a page lives inside a project. */
export function inProject(view: View): view is ProjectSection | 'task' {
  return !isGlobal(view);
}

const enc = encodeURIComponent;

export function globalHref(view: GlobalView): string {
  return `#/${view}`;
}

export function projectHref(projectId: string, section: ProjectSection = 'board'): string {
  return `#/p/${enc(projectId)}/${section}`;
}

export function taskHref(projectId: string, taskId: string): string {
  return `#/p/${enc(projectId)}/task/${enc(taskId)}`;
}

/** The address of a location. A project page whose project is not known yet has none to give. */
export function hrefOf(loc: Location): string {
  if (loc.view === 'task') return taskHref(loc.projectId, loc.taskId);
  if (isGlobal(loc.view)) return globalHref(loc.view);
  if (loc.view === 'git' && loc.sub) return `${projectHref(loc.projectId, 'git')}/${loc.sub}`;
  return projectHref(loc.projectId, loc.view);
}

/**
 * Where switching to another project goes. The section is kept where it makes
 * sense: from a project's Board, Git or Activity you land on the same one of the
 * new project. A task belongs to the project it is in, so from its page you land
 * on the new project's board, and from a global page you enter the new project at
 * its board.
 */
export function switchedTo(from: Location, projectId: string): Location {
  const section: ProjectSection = isSection(from.view) ? from.view : 'board';
  return { view: section, projectId, taskId: '' };
}

/**
 * Settles a location that does not name a project: an old link, or no link at all.
 * Such a page is in the project last used, or, if it is not (any longer) a project
 * that exists, in the first one. With no projects there is nothing to be in, and
 * the Projects page is where one is registered.
 */
export function resolved(loc: Location, projectIds: readonly string[], lastUsed: string): Location {
  if (!inProject(loc.view) || loc.projectId) return loc;
  const id = projectIds.includes(lastUsed) ? lastUsed : (projectIds[0] ?? '');
  if (!id) return { view: 'projects', projectId: '', taskId: '' };
  return { ...loc, projectId: id };
}
