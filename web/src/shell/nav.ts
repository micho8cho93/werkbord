// Workspace-owned detail routes remain available through the command palette.
// Primary navigation and project context live in Sidebar.svelte and project-context.ts.
import type { IconName } from '../lib/Icon.svelte';
import type { Item, Kind, Overview, Project } from './types';

export type NavKey = '' | 'home' | 'projects' | 'reviews' | 'members' | 'analytics' | `project:${string}`;

export interface NavEntry {
  key: NavKey;
  title: string;
  icon: IconName;
  /** A place inside the workspace: what the sidebar asks it to show. */
  place: string;
}

/** Workspace detail shortcuts for the palette. Settings is the window’s own screen. */
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

/** The projects the sidebar lists: as the workspace reported them, without duplicates, in its order. */
export function listed(projects: Project[] | undefined): Project[] {
  const seen = new Set<string>();
  return (projects ?? []).filter((p) => !seen.has(p.id) && !!seen.add(p.id));
}

// ---- accessible workspace metadata ----

/** Personal is the person's own colour; each team gets one of these, the same one every time (it is chosen from the team's id). */
const TEAM_COLOURS = ['#7B4FC0', '#0A7C86', '#A0457E', '#5F7F2A'] as const;

/** The colour that marks a place wherever it is named: Personal is blue, and a team is one of a few other hues. */
export function sectionColour(kind: Kind, id: string): string {
  if (kind === 'personal') return 'var(--accent)';
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return TEAM_COLOURS[h % TEAM_COLOURS.length];
}

/** Accessible workspaces for palette and report filters, excluding setup and leaving entries. */
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
    | { page: 'inbox' | 'calendar' | 'agents' | 'reviews' | 'analytics' | 'workspaces' | 'settings' }
    | { workspace: string; place: string }
    | { addTeam: true };
}

/** Every place the palette can go to: the window's own pages, and for each workspace its home, projects and places. */
export function destinations(items: readonly Item[] | undefined, overview: Overview | null): Destination[] {
  const out: Destination[] = [
    { id: 'page:inbox', title: 'Work', hint: 'Everything that needs your attention', go: { page: 'inbox' } },
    { id: 'page:calendar', title: 'Calendar', hint: 'Across Personal and your teams', go: { page: 'calendar' } },
    { id: 'page:agents', title: 'Agents', hint: 'Activity in the selected project context', go: { page: 'agents' } },
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

/** The Individual page that holds Agents, with the session of this key open (agent-sessions.ts), or the list alone. */
export const agentsPlace = (key = ''): string => (key ? `#/sessions/${encodeURIComponent(key)}` : '#/sessions');
/** Whether a place inside the person's own Werkbord is Agents. */
export const isAgentsPlace = (place: string): boolean => /^#\/sessions(?:[/?]|$)/.test(place);
