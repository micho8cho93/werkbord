// The sections of Settings, listed once for the Mac window page and for the web app. Each side builds its own screen from this list, so a
// section cannot be named, grouped or ordered differently in one of them. What a section holds is its own component; what is here is only
// what it is called, where it sits, and where it can be shown.

/**
 * Where a section can be shown. `everywhere`: the web app in a browser, on a phone and in the Mac app. `app`: only where the Mac app is
 * present (it asks the app for what it shows). `window`: only the Mac window's own page, until the web app can draw it.
 */
export type Reach = 'everywhere' | 'app' | 'window';

export interface SectionDef {
  /** The section's name in an address (`#/settings?runners`) and in the window's list. */
  key: string;
  label: string;
  reach?: Reach;
}

export interface GroupDef {
  label: string;
  sections: SectionDef[];
}

/** Projects and workspaces are one group of Settings: the workspaces on this computer, and each one's projects. */
export const PROJECTS_LABEL = 'Projects & workspaces';

/** What is the same wherever Settings is opened, because it is about this device and this person: how it looks, what it tells you, and what the app is. */
export const GENERAL: GroupDef = {
  label: 'General',
  sections: [
    { key: 'appearance', label: 'Appearance' },
    { key: 'alerts', label: 'Alerts' },
    { key: 'shortcuts', label: 'Keyboard shortcuts' },
    { key: 'about', label: 'Updates & about', reach: 'app' },
    { key: 'plan', label: 'Plan', reach: 'window' },
  ],
};

/** The workspaces on this computer, and where a team is made or joined. */
export const WORKSPACES: SectionDef = { key: 'workspaces', label: 'Workspaces', reach: 'window' };

/** Individual's own sections. */
export const PERSONAL: GroupDef[] = [
  { label: PROJECTS_LABEL, sections: [{ key: 'history', label: 'Project history' }] },
  { label: 'Work', sections: [{ key: 'defaults', label: 'Task defaults' }, { key: 'routing', label: 'Routing rules' }, { key: 'time', label: 'Time format' }] },
  { label: 'Machines', sections: [{ key: 'runners', label: 'Runners' }, { key: 'agents', label: 'Agents' }, { key: 'usage', label: 'Usage' }, { key: 'computer', label: 'This computer' }] },
  { label: 'Connections', sections: [{ key: 'phones', label: 'Phones' }, { key: 'github', label: 'GitHub' }, { key: 'mcp', label: 'MCP services' }, { key: 'memory', label: 'Personal memory' }] },
];

/** Where a screen is drawn: in the Mac app (`app`), and as the Mac window's own page (`window`). */
export interface Surface {
  app: boolean;
  window: boolean;
}

export const shownOn = (s: SectionDef, on: Surface): boolean => s.reach === undefined || s.reach === 'everywhere' || (s.reach === 'app' ? on.app : on.window);

/** The groups with only the sections that can be shown on a surface; a group with none is left out. */
export function visibleGroups(groups: readonly GroupDef[], on: Surface): GroupDef[] {
  return groups.map((g) => ({ ...g, sections: g.sections.filter((s) => shownOn(s, on)) })).filter((g) => g.sections.length);
}

/** An address of Individual's Settings for a section: the first, Task defaults, is the page itself. */
export const settingsQuery = (key: string): string => (key === 'defaults' ? '' : `?${key}`);
