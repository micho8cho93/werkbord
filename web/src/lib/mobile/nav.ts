// Where Back goes on the phone, and what each page is called there. Pure, so it can be tested on its own.

import { globalHref, hrefOf, type Location, type View } from '../location';

/** What a page is called in the phone's heading and in the words "Back to …". */
const TITLES: Partial<Record<View, string>> = {
  deck: 'Control Deck',
  control: 'Needs you',
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
 * The page above this one, when nothing has been visited before it (a link opened from outside, or a reload). A key's page sits
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

/** The address Back goes to when it cannot go to the page before. */
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

/**
 * What the trail of pages visited does when the address changes to `href`: going to the page before the current one is going back
 * (it is dropped from the trail); anything else is going on. Back then follows the person's own steps, as the phone's own button
 * does, and falls back to the parent only when there are none.
 */
export function advance(trail: readonly string[], href: string, replace = false): string[] {
  if (trail[trail.length - 1] === href) return [...trail];
  if (replace) return [...trail.slice(0, -1), href];
  if (trail.length >= 2 && trail[trail.length - 2] === href) return trail.slice(0, -1);
  return [...trail, href].slice(-30);
}
