// What Settings lists, for the part of Werkbord the person is in. Pure: the Settings page is built from this.
//
// Settings is one screen whichever place the person comes from. The same groups appear everywhere, and a section appears only
// where it changes something of that place: Personal's task defaults are not a team's, a team's labels and hosts are not
// Personal's. Home lists everything, a workspace at a time. A section is either the shell's own (Appearance, Alerts) or a
// page of the workspace it belongs to, shown here without that page's own navigation.

import type { Item, Kind } from './types';

/** Which part of Werkbord Settings is for: Home (everything), Personal, or one team (its id). */
export type Scope = { kind: 'universal' } | { kind: 'personal' } | { kind: 'team'; id: string };

export type Native = 'appearance' | 'alerts' | 'plan' | 'workspaces';

export type Where =
  | { native: Native }
  /** A page of a workspace, shown without its own navigation: a place such as `#/settings?phones` or `?tab=settings&part=phones`. */
  | { workspace: string; place: string };

export interface Section {
  /** Unique across the whole list, including between two Team workspaces. */
  id: string;
  label: string;
  where: Where;
}

export interface Group {
  /** Shown above the sections; the same words in every scope. */
  label: string;
  /** Which workspace a group of Home's list belongs to, when there is more than one to tell apart. */
  owner?: string;
  /** Optional hosting configuration, revealed on demand or by a direct section link. */
  advanced?: boolean;
  sections: Section[];
}

const PERSONAL = 'personal';

/** Projects and workspaces are one section of Settings of their own: the workspaces on this computer, and each one's projects. */
const PROJECTS = 'Projects & workspaces';

const here = (workspace: string, place: string): Where => ({ workspace, place });

/** Individual's sections, in the groups Settings always has. */
function personal(): Group[] {
  const at = (id: string, label: string, query: string): Section => ({ id: `personal/${id}`, label, where: here(PERSONAL, `#/settings${query}`) });
  return [
    { label: PROJECTS, sections: [at('history', 'Project history', '?history')] },
    { label: 'Work', sections: [at('defaults', 'Task defaults', ''), at('routing', 'Routing rules', '?routing'), at('time', 'Time format', '?time')] },
    { label: 'Machines', sections: [at('runners', 'Runners', '?runners'), at('agents', 'Agents', '?agents'), at('computer', 'This computer', '?computer')] },
    { label: 'Connections', sections: [at('phones', 'Phones', '?phones'), at('github', 'GitHub', '?github'), at('mcp', 'MCP services', '?mcp'), at('memory', 'Personal memory', '?memory')] },
  ];
}

/** A Team workspace's sections, in the same groups. */
function team(id: string): Group[] {
  const at = (key: string, label: string, query: string): Section => ({ id: `${id}/${key}`, label, where: here(id, `?tab=${query}`) });
  return [
    { label: PROJECTS, sections: [at('history', 'Project history', 'projects')] },
    { label: 'Work', sections: [at('labels', 'Labels', 'labels')] },
    {
      label: 'Machines',
      sections: [at('computer', 'This computer', 'settings&part=computer'), at('devices', 'Devices', 'devices')],
    },
    { label: 'Connections', sections: [at('phones', 'Phones', 'settings&part=phones'), at('mcp', 'MCP clients', 'mcp'), at('memory', 'Team memory', 'settings&part=memory')] },
    { label: 'Workspace', sections: [at('backups', 'Backups', 'backups')] },
    { label: 'Advanced', advanced: true, sections: [at('hosts', 'Workspace Hosts', 'hosts'), at('connectivity', 'Connectivity', 'connectivity')] },
  ];
}

/** The shell's own: what is the same in every workspace because it is about this window and this person. */
export function general(): Group {
  return {
    label: 'General',
    sections: [
      { id: 'general/appearance', label: 'Appearance', where: { native: 'appearance' } },
      { id: 'general/alerts', label: 'Alerts', where: { native: 'alerts' } },
      { id: 'general/plan', label: 'Plan', where: { native: 'plan' } },
    ],
  };
}

/** The workspaces on this computer, and where a team is made or joined: the shell's own, the first of Projects & workspaces. */
export function workspacesGroup(): Group {
  return { label: PROJECTS, sections: [{ id: 'general/workspaces', label: 'Workspaces', where: { native: 'workspaces' } }] };
}

/** Inside one workspace's own scope the workspaces list and that workspace's projects are one group. */
function withWorkspaces(groups: Group[]): Group[] {
  return groups.map((g) => (g.label === PROJECTS ? { ...g, sections: [...workspacesGroup().sections, ...g.sections] } : g));
}

/** The Team workspaces whose settings can be shown: the ones the window has open or can open. */
export function teamsOf(items: readonly Item[] | undefined): Item[] {
  return (items ?? []).filter((i) => i.kind === 'team' && (i.state === 'ready' || i.state === 'connecting' || i.state === 'offline'));
}

/** What Settings lists for a scope. */
export function groupsFor(scope: Scope, items: readonly Item[] | undefined): Group[] {
  if (scope.kind === 'personal') return [general(), ...withWorkspaces(personal())];
  if (scope.kind === 'team') return [general(), ...withWorkspaces(team(scope.id))];
  const out: Group[] = [general(), workspacesGroup()];
  out.push(...personal().map((g) => ({ ...g, owner: 'Personal' })));
  for (const t of teamsOf(items)) out.push(...team(t.id).map((g) => ({ ...g, owner: t.name })));
  return out;
}

/** Every section of a list of groups. */
export function sectionsOf(groups: readonly Group[]): Section[] {
  return groups.flatMap((g) => g.sections);
}

/** The section to show: the one asked for if this scope has it, else the first (Appearance). */
export function pick(groups: readonly Group[], id: string | undefined): Section {
  const all = sectionsOf(groups);
  return all.find((s) => s.id === id) ?? all[0];
}

/** The scope a workspace's place gives Settings, for the one open now. */
export function scopeOf(universal: boolean, current: string, kind: Kind | undefined): Scope {
  if (universal) return { kind: 'universal' };
  return kind === 'team' ? { kind: 'team', id: current } : { kind: 'personal' };
}

/** Which workspace's frame a section needs, if it has one. */
export function frameOf(s: Section): string | undefined {
  return 'workspace' in s.where ? s.where.workspace : undefined;
}

/**
 * The section a place of a workspace's own page belongs to: `#/settings?phones` of Individual, `?tab=devices` or
 * `?tab=settings&part=phones` of a Team. Anything that names no section of its own (`#/settings`, `?tab=settings`) is the
 * workspace's first.
 */
export function sectionForPlace(groups: readonly Group[], workspace: string, place: string): Section | undefined {
  const mine = sectionsOf(groups).filter((s) => 'workspace' in s.where && s.where.workspace === workspace);
  const key = (p: string): string => {
    const q = p.includes('?') ? p.slice(p.indexOf('?') + 1) : '';
    if (workspace === PERSONAL) return q.split('&')[0];
    const params = new URLSearchParams(q);
    const tab = params.get('tab') ?? '';
    return tab === 'settings' ? `settings:${params.get('part') ?? 'computer'}` : tab;
  };
  const wanted = key(place);
  const first = mine.find((s) => s.id === `${workspace}/${workspace === PERSONAL ? 'defaults' : 'labels'}`) ?? mine[0];
  return mine.find((s) => 'workspace' in s.where && key(s.where.place) === wanted) ?? first;
}
