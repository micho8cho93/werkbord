// Which page the address bar names, and how to name one. Pure, so it can be
// tested on its own.
//
// Project is the scope of the application. There are two kinds of page:
//
//   global          #/control          what needs you, in every project
//                   #/projects         every registered repository, and registering one
//                   #/help             bundled task guidance and keyboard shortcuts
//   in a project    #/p/<id>/board     (also: overview, calendar, git, runs, defaults)
//                   #/p/<id>/git/...   Git drills down: branch, file, changes (see gitroute.ts)
//                   #/p/<id>/task/<id> a task, which belongs to the board
//
// A project's own pages are listed once, in PROJECT_SECTIONS. The shell, the
// switcher and the tab bar are all built from that list, so a section added to
// it (Calendar, in V1) appears everywhere without further navigation work.

export type GlobalView = 'control' | 'projects' | 'settings' | 'help' | 'onboarding' | 'analytics' | 'personal' | PhoneView;
/**
 * The pages only the phone has: the Control Deck that opens it and the screens its keys lead to. They are global (they look across
 * every project), they are not in GLOBAL_VIEWS (the desktop navigation does not list them), and an address that names one on a wide
 * window is settled to Needs you (App.svelte). Needs you, Projects and Analytics are the deck's other keys, and are the desktop's own pages.
 */
export type PhoneView = 'deck' | 'mywork' | 'schedule' | 'agents' | 'reviews' | 'repositories' | 'runners';
export const PHONE_VIEWS: readonly PhoneView[] = ['deck', 'mywork', 'schedule', 'agents', 'reviews', 'repositories', 'runners'];
export type ProjectSection = 'product-map' | 'overview' | 'board' | 'calendar' | 'timeline' | 'git' | 'runs' | 'analytics' | 'defaults';
export type View = GlobalView | ProjectSection | 'task';

export const GLOBAL_VIEWS: readonly { id: Exclude<GlobalView, 'onboarding' | 'personal' | PhoneView>; label: string; short: string }[] = [
  { id: 'control', label: 'Workspace', short: 'Workspace' },
  { id: 'projects', label: 'Projects', short: 'Projects' },
  { id: 'analytics', label: 'Analytics', short: 'Analytics' },
  { id: 'settings', label: 'Settings', short: 'Settings' },
  { id: 'help', label: 'Help', short: 'Help' },
];

/** Direct-only pages: first-time setup and saved personal workspaces. */
const HIDDEN_GLOBAL: readonly GlobalView[] = ['onboarding', 'personal'];

export const PROJECT_SECTIONS: readonly { id: ProjectSection; label: string }[] = [
  { id: 'overview', label: 'Overview' },
  { id: 'board', label: 'Board' },
  { id: 'calendar', label: 'Calendar' },
  { id: 'timeline', label: 'Timeline' },
  { id: 'git', label: 'Git' },
  { id: 'product-map', label: 'Product Map' },
  { id: 'runs', label: 'Runs' },
  { id: 'analytics', label: 'Analytics' },
  // The project's defaults (agent, model, reasoning, interaction, priority). Its address says "defaults" so
  // that it cannot be mistaken for the app's own Settings page, which is global.
  { id: 'defaults', label: 'Project settings' },
];

/** The sections shown as the project's tabs. Its settings are reached from the gear in the project header. */
export const PROJECT_TABS = PROJECT_SECTIONS.filter((s) => !['defaults', 'timeline', 'analytics'].includes(s.id));

/**
 * Sections that exist only for a project with a Git repository: the calendar schedules agents, Git is
 * the repository, and runs are agents' sessions. A work project has none of them.
 */
const REPOSITORY_ONLY: readonly ProjectSection[] = ['product-map'];

/** Whether a section is there for a project of this kind. */
export function sectionApplies(section: ProjectSection, kind: string | undefined): boolean {
  return kind !== 'work' || !REPOSITORY_ONLY.includes(section);
}

/** The project's sections, for its kind. */
export function sectionsFor(kind: string | undefined): readonly { id: ProjectSection; label: string }[] {
  return PROJECT_SECTIONS.filter((s) => sectionApplies(s.id, kind));
}

/** The project's tabs, for its kind. */
export function tabsFor(kind: string | undefined): readonly { id: ProjectSection; label: string }[] {
  return PROJECT_TABS.filter((s) => sectionApplies(s.id, kind));
}

export interface Location {
  view: View;
  /** The project the page is in; empty on a global page, and on an old link that does not say. */
  projectId: string;
  /** Set on a task's page. */
  taskId: string;
  /** A task page can select one of its earlier runs without changing the task address. */
  runId?: string;
  /** Git's drill-down: whatever follows `git/` in the address, with its query. Absent on every other page. */
  sub?: string;
}

const isGlobal = (v: string): v is GlobalView => GLOBAL_VIEWS.some((g) => g.id === v) || (HIDDEN_GLOBAL as readonly string[]).includes(v) || (PHONE_VIEWS as readonly string[]).includes(v);
/** Whether a page is one of the phone's own. */
export const isPhoneView = (v: string): v is PhoneView => (PHONE_VIEWS as readonly string[]).includes(v);
const isSection = (v: string): v is ProjectSection => PROJECT_SECTIONS.some((s) => s.id === v);

/** Sections that were renamed: an old link still lands. "Activity" was the run history, now Runs. */
const RENAMED: Record<string, ProjectSection> = { activity: 'runs' };
const section = (v: string): ProjectSection | '' => (isSection(v) ? v : (RENAMED[v] ?? ''));

/** A path part decoded, or '' when it is not valid percent-encoding. */
export function decode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return '';
  }
}

/** What follows `?` in the address: a page's own state, such as the Settings section shown. */
export function queryOf(hash: string): string {
  const i = hash.indexOf('?');
  return i === -1 ? '' : hash.slice(i + 1);
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
      const runId = new URLSearchParams(queryOf(hash)).get('run') ?? '';
      return taskId ? { view: 'task', projectId, taskId, ...(runId ? { runId } : {}) } : { view: 'board', projectId, taskId: '' };
    }
    if (third === 'git') {
      // Git has screens of its own below it. Everything after `git/`, and the query, is theirs to read.
      const q = full.indexOf('?');
      const rest = full.split(/[?]/)[0].split('/').slice(3).join('/');
      const sub = rest + (q === -1 ? '' : full.slice(q));
      return sub ? { view: 'git', projectId, taskId: '', sub } : { view: 'git', projectId, taskId: '' };
    }
    return { view: section(third) || 'board', projectId, taskId: '' };
  }

  // Older links named a section without a project: it is the one last used.
  if (section(first)) return { view: section(first) as ProjectSection, projectId: '', taskId: '' };
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

export function taskHref(projectId: string, taskId: string, runId = ''): string {
  const path = `#/p/${enc(projectId)}/task/${enc(taskId)}`;
  return runId ? `${path}?${new URLSearchParams({ run: runId })}` : path;
}

/** The address of a location. A project page whose project is not known yet has none to give. */
export function hrefOf(loc: Location): string {
  if (loc.view === 'task') return taskHref(loc.projectId, loc.taskId, loc.runId);
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
  const section: ProjectSection = !isGlobal(from.view) && isSection(from.view) ? from.view : 'board';
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

/** A page the phone's "More" sheet reaches: the ones the tab bar has no room for. */
export interface MoreEntry {
  id: 'analytics' | 'overview' | 'projects' | 'defaults' | 'settings' | 'help';
  label: string;
  href: string;
}

/**
 * The phone's tab bar holds Needs you and the project's work sections. Everything else is behind its last tab,
 * "More": the project's Overview and settings when a project is open, then Projects, Settings and Help.
 */
export function moreEntries(projectId: string): MoreEntry[] {
  return [
    ...(projectId
      ? ([
          { id: 'overview', label: 'Overview', href: projectHref(projectId, 'overview') },

          { id: 'defaults', label: 'Project settings', href: projectHref(projectId, 'defaults') },
        ] as MoreEntry[])
      : []),
    { id: 'projects', label: 'Projects', href: globalHref('projects') },
    { id: 'analytics', label: 'Analytics', href: globalHref('analytics') },
    { id: 'settings', label: 'Settings', href: globalHref('settings') },
    { id: 'help', label: 'Help', href: globalHref('help') },
  ];
}

/** Whether the page shown is one of those behind More, so the tab can say where you are. */
export function inMore(view: View): boolean {
  return view === 'analytics' || view === 'overview' || view === 'defaults' || view === 'projects' || view === 'settings' || view === 'help';
}
