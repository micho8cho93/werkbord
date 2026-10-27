// What the window's one sidebar lists: a section for Personal and one for each team, each with its projects, and which entry a
// workspace's current place belongs to.
// Where the person is has two levels. The sidebar lists the major parts (a workspace, and each project in it); inside a major part
// its own pages carry one row of tabs for its sub-areas (a project's Board and Git, a team's Projects and People). A sub-area is
// never a sidebar entry.
// Pure: the sidebar is built from this, and the workspace's own pages carry no navigation of their own when they are shown
// here. A place is what the workspace itself reports (a fragment for Personal, a query for a team); the shell never reads
// anything else of it.

import type { IconName } from '../lib/Icon.svelte';
import { decode } from '../lib/location';
import type { Item, Kind, Overview, Project } from './types';

export type NavKey = '' | 'home' | 'projects' | 'reviews' | 'members' | 'analytics' | `project:${string}`;

export interface NavEntry {
  key: NavKey;
  title: string;
  icon: IconName;
  /** A place inside the workspace: what the sidebar asks it to show. */
  place: string;
}

/** The fixed entries of a workspace, in order. Its projects are listed under `projects`; Settings is the window's own screen. */
export function entries(kind: Kind): NavEntry[] {
  if (kind === 'personal') {
    return [
      { key: 'home', title: 'Workspace', icon: 'overview', place: '#/control' },
      { key: 'projects', title: 'Projects', icon: 'folder', place: '#/projects' },
      { key: 'analytics', title: 'Analytics', icon: 'analytics', place: '#/analytics' },
    ];
  }
  return [
    { key: 'home', title: 'Workspace', icon: 'overview', place: '?tab=workspace' },
    { key: 'projects', title: 'Projects', icon: 'folder', place: '?tab=projects' },
    { key: 'reviews', title: 'Reviews', icon: 'merge', place: '?tab=reviews' },
    { key: 'members', title: 'People', icon: 'people', place: '?tab=members' },
    { key: 'analytics', title: 'Analytics', icon: 'analytics', place: '?tab=analytics' },
  ];
}

const TEAM_PROJECT_TABS = new Set(['overview', 'board', 'calendar', 'timeline', 'repository', 'activity', 'project-settings', 'people']);

/** The entry a workspace's current place belongs to; '' when it is somewhere the sidebar does not list. */
export function activeKey(kind: Kind, place: string | undefined): NavKey {
  if (!place) return '';
  if (kind === 'personal') {
    const [first = '', second = ''] = place.replace(/^#\/?/, '').split(/[?&]/)[0].split('/');
    if (first === 'control') return 'home';
    if (first === 'projects') return 'projects';
    if (first === 'analytics') return 'analytics';
    if (first === 'p' && decode(second)) return `project:${decode(second)}`;
    return '';
  }
  if (!place.startsWith('?')) return '';
  const q = new URLSearchParams(place);
  const tab = q.get('tab') ?? 'workspace';
  if (tab === 'workspace') return 'home';
  if (tab === 'projects') return 'projects';
  if (tab === 'reviews' || tab === 'members' || tab === 'analytics') return tab;
  const project = q.get('project');
  return TEAM_PROJECT_TABS.has(tab) && project ? `project:${project}` : '';
}

/** The projects the sidebar lists: as the workspace reported them, without duplicates, in its order. */
export function listed(projects: Project[] | undefined): Project[] {
  const seen = new Set<string>();
  return (projects ?? []).filter((p) => !seen.has(p.id) && !!seen.add(p.id));
}

/** Display associations never confer Team permissions. One ID produces one row in a context. */
export function generatedEntries(workspaces: import('../lib/personal/schema').Workspace[], universal: boolean, context: string) {
  const seen = new Set<string>();
  return workspaces.filter(w => !seen.has(w.id) && !!seen.add(w.id) && (universal
    ? w.visibility.universal
    : context === 'personal' ? w.visibility.individual : w.visibility.teamWorkspaceIds?.includes(context)));
}

/** Salie's saved-workspace links may include their approved revision as a reload hint. */
export function generatedWorkspaceID(place: string): string | undefined {
  return /^#\/personal\?id=(pws_[a-z2-7]{16})(?:&revision=[1-9][0-9]{0,15})?$/.exec(place)?.[1];
}

/** The place inside a workspace that a shortcut to one of its own pages opens. */
export function shellPlace(kind: Kind, target: 'home' | 'projects' | 'analytics'): string {
  const fixed = entries(kind);
  if (target === 'home') return fixed[0].place;
  if (target === 'projects') return fixed[1].place;
  return kind === 'personal' ? '#/analytics' : '?tab=analytics';
}

// ---- the sections of the sidebar ----

/** Personal is the person's own colour; each team gets one of these, the same one every time (it is chosen from the team's id). */
const TEAM_COLOURS = ['#7B4FC0', '#0A7C86', '#A0457E', '#5F7F2A'] as const;

/** The colour that marks a place wherever it is named: Personal is blue, and a team is one of a few other hues. */
export function sectionColour(kind: Kind, id: string): string {
  if (kind === 'personal') return 'var(--accent)';
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return TEAM_COLOURS[h % TEAM_COLOURS.length];
}

/** The workspaces that have a section: not one that is only being set up or has been left. Personal first, then the teams as listed. */
export function sectionItems(items: readonly Item[] | undefined): Item[] {
  const shown = (items ?? []).filter((i) => i.state !== 'setup' && i.state !== 'leaving');
  return [...shown.filter((i) => i.kind === 'personal'), ...shown.filter((i) => i.kind === 'team')];
}

/** The sub-areas of a workspace that only the palette names (the workspace shows them as tabs): Projects, and People for a team. */
export function subAreas(kind: Kind): NavEntry[] {
  return entries(kind).filter((e) => e.key === 'projects' || e.key === 'members');
}

/** Something the go-to palette can take the person to. */
export interface Destination {
  id: string;
  title: string;
  /** Where it is, in words: "Acme · project", "Personal". */
  hint: string;
  go:
    | { page: 'inbox' | 'calendar' | 'reviews' | 'analytics' | 'workspaces' | 'personal-library' | 'settings' }
    | { workspace: string; place: string }
    | { addTeam: true };
}

/** Every place the palette can go to: the window's own pages, and for each workspace its home, projects and places. */
export function destinations(items: readonly Item[] | undefined, overview: Overview | null): Destination[] {
  const out: Destination[] = [
    { id: 'page:inbox', title: 'Home', hint: 'Everything that needs your attention', go: { page: 'inbox' } },
    { id: 'page:calendar', title: 'Calendar', hint: 'Across Personal and your teams', go: { page: 'calendar' } },
    { id: 'page:reviews', title: 'Reviews', hint: 'Work waiting on a review', go: { page: 'reviews' } },
    { id: 'page:analytics', title: 'Analytics', hint: 'How work is going, by place and period', go: { page: 'analytics' } },
  ];
  for (const i of sectionItems(items)) {
    const where = i.kind === 'personal' ? 'Personal' : i.name;
    const kind = i.kind;
    const home = entries(kind)[0];
    out.push({ id: `ws:${i.id}`, title: where, hint: kind === 'personal' ? 'Your own work' : 'Team', go: { workspace: i.id, place: home.place } });
    const summary = overview?.entries.find((e) => e.workspace.id === i.id)?.summary;
    for (const p of listed(summary?.projects)) out.push({ id: `project:${i.id}:${p.id}`, title: p.name, hint: `${where} · project`, go: { workspace: i.id, place: p.href } });
    for (const e of subAreas(kind)) out.push({ id: `place:${i.id}:${e.key}`, title: e.title, hint: where, go: { workspace: i.id, place: e.place } });
  }
  out.push(
    { id: 'page:workspaces', title: 'Devices & Hosting', hint: 'This computer, hosts and runners', go: { page: 'workspaces' } },
    { id: 'page:personal-library', title: 'Data library', hint: 'Collections your workspaces share', go: { page: 'personal-library' } },
    { id: 'page:settings', title: 'Settings', hint: 'Appearance, alerts, plan and more', go: { page: 'settings' } },
    { id: 'add-team', title: 'New team or join…', hint: 'Create a team, or join one with a link', go: { addTeam: true } },
  );
  return out;
}

/** The destinations a typed query matches: every word of it must be in the title or the hint. An empty query matches all, in order. */
export function matching(all: readonly Destination[], query: string): Destination[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return [...all];
  const scored = all
    .map((d, order) => {
      const title = d.title.toLowerCase();
      const text = `${title} ${d.hint.toLowerCase()}`;
      if (!words.every((w) => text.includes(w))) return null;
      // A title that starts with the query comes first, then one that contains it, then one only the hint matches.
      const rank = words.every((w) => title.startsWith(w)) ? 0 : words.every((w) => title.includes(w)) ? 1 : 2;
      return { d, rank, order };
    })
    .filter((x): x is { d: Destination; rank: number; order: number } => x !== null);
  return scored.sort((a, b) => a.rank - b.rank || a.order - b.order).map((x) => x.d);
}
