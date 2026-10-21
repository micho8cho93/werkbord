// Where Back goes on the phone, and what each page is called there. Pure, so it can be tested on its own.

import { globalHref, hrefOf, type Location, type View } from '../location';

/** What a page is called in the phone's heading and in the words "Back to …". */
const TITLES: Partial<Record<View, string>> = {
  deck: 'Control Deck',
  control: 'Workspace',
  schedule: 'Calendar',
  projects: 'Projects',
  mywork: 'My Work',
  agents: 'Agents',
  analytics: 'Analytics',
  reviews: 'Reviews',
  repositories: 'Git',
  runners: 'Runners',
  settings: 'Settings',
  help: 'Help',
  onboarding: 'Set up Werkbord',
};

export const titleOf = (view: View): string => TITLES[view] ?? '';

/**
 * The page above this one in the navigation tree. A key's page sits
 * directly under the deck; a project's page sits under the list of projects; a task sits on its project's board; Git's screens sit
 * on Git. The deck itself has nothing above it.
 */
export function phoneParent(loc: Location): Location | null {
  if (loc.view === 'deck') return null;
  if (loc.view === 'task') return { view: 'board', projectId: loc.projectId, taskId: '' };
  if (loc.view === 'git' && loc.sub) return { view: 'git', projectId: loc.projectId, taskId: '' };
  if (loc.projectId) return { view: 'projects', projectId: '', taskId: '' };
  return { view: 'deck', projectId: '', taskId: '' };
}

/** The address the app’s Back button always goes to. */
export const parentHref = (loc: Location): string => {
  const parent = phoneParent(loc);
  return parent ? hrefOf(parent) : globalHref('deck');
};

/** The words for the page Back would go to: "Projects", "Control Deck", "Board". */
export function parentTitle(loc: Location): string {
  const parent = phoneParent(loc);
  if (!parent) return '';
  return parent.view === 'board' ? 'Board' : (titleOf(parent.view) || 'Back');
}
