import type { IconName } from '../Icon.svelte';
import type { ShortcutTarget } from './schema';

/** What a shortcut offers. The window decides which of these it can open: a shortcut only ever names one of them. */
export const shortcutInfo: Record<ShortcutTarget, { label: string; about: string; icon: IconName }> = {
  home: { label: 'Workspace home', about: 'What needs your attention', icon: 'overview' },
  projects: { label: 'Projects', about: 'Repositories, boards and tasks', icon: 'folder' },
  analytics: { label: 'Analytics', about: 'Usage, cost and activity', icon: 'analytics' },
  calendar: { label: 'Calendar', about: 'Everything scheduled', icon: 'calendar' },
  'data-library': { label: 'Data library', about: 'The collections behind workspaces', icon: 'database' },
  settings: { label: 'Settings', about: 'Preferences for this computer', icon: 'settings' },
  assistant: { label: 'Ask Salie', about: 'Describe a change, or ask a question', icon: 'assistant' },
};

/** Where each shortcut goes in the app's own pages (the address of the page). Shown outside the desktop window, in a browser. */
export const pageAddresses: Partial<Record<ShortcutTarget, string>> = {
  home: '#/control', projects: '#/projects', analytics: '#/analytics', settings: '#/settings',
};
