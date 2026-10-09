// What the window's one sidebar lists for the workspace on show, and which entry that workspace's current place belongs to.
// Pure: the sidebar is built from this, and the workspace's own pages carry no navigation of their own when they are shown
// here. A place is what the workspace itself reports (a fragment for Individual, a query for Team); the shell never reads
// anything else of it.

import type { IconName } from '../lib/Icon.svelte';
import type { Kind, Project } from './types';

export type NavKey = '' | 'home' | 'projects' | 'reviews' | 'members' | 'settings' | `project:${string}`;

export interface NavEntry {
  key: NavKey;
  title: string;
  icon: IconName;
  /** A place inside the workspace: what the sidebar asks it to show. */
  place: string;
}

/** The fixed entries of a workspace, in order. Its projects are listed under `projects`; Settings sits with the window's own. */
export function entries(kind: Kind): NavEntry[] {
  if (kind === 'personal') {
    return [
      { key: 'home', title: 'Control Center', icon: 'overview', place: '#/control' },
      { key: 'projects', title: 'Projects', icon: 'folder', place: '#/projects' },
    ];
  }
  return [
    { key: 'home', title: 'Workspace', icon: 'overview', place: '?tab=workspace' },
    { key: 'projects', title: 'Projects', icon: 'folder', place: '?tab=projects' },
    { key: 'reviews', title: 'Reviews', icon: 'merge', place: '?tab=reviews' },
    { key: 'members', title: 'Members', icon: 'people', place: '?tab=members' },
  ];
}

/** Where a workspace's Settings are. */
export function settingsPlace(kind: Kind): string {
  return kind === 'personal' ? '#/settings' : '?tab=settings';
}

/** Where a workspace's runners are: Individual lists them under Settings; Team has no such list to point at. */
export function runnersPlace(kind: Kind): string | undefined {
  return kind === 'personal' ? '#/settings?runners' : undefined;
}

const TEAM_PROJECT_TABS = new Set(['board', 'repository', 'activity', 'people']);
const TEAM_SETTINGS = new Set(['settings', 'devices', 'hosts', 'connectivity', 'backups', 'license']);

function decode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return '';
  }
}

/** The entry a workspace's current place belongs to; '' when it is somewhere the sidebar does not list. */
export function activeKey(kind: Kind, place: string | undefined): NavKey {
  if (!place) return '';
  if (kind === 'personal') {
    const [first = '', second = ''] = place.replace(/^#\/?/, '').split(/[?&]/)[0].split('/');
    if (first === 'control') return 'home';
    if (first === 'projects') return 'projects';
    if (first === 'settings') return 'settings';
    if (first === 'p' && decode(second)) return `project:${decode(second)}`;
    return '';
  }
  if (!place.startsWith('?')) return '';
  const q = new URLSearchParams(place);
  const tab = q.get('tab') ?? 'workspace';
  if (tab === 'workspace') return 'home';
  if (tab === 'projects') return 'projects';
  if (tab === 'reviews' || tab === 'members') return tab;
  if (TEAM_SETTINGS.has(tab)) return 'settings';
  const project = q.get('project');
  return TEAM_PROJECT_TABS.has(tab) && project ? `project:${project}` : '';
}

/** The projects the sidebar lists: as the workspace reported them, without duplicates, in its order. */
export function listed(projects: Project[] | undefined): Project[] {
  const seen = new Set<string>();
  return (projects ?? []).filter((p) => !seen.has(p.id) && !!seen.add(p.id));
}
