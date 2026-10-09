/**
 * Inside the Werkbord desktop app this web app can be one workspace among several: the app's shell shows it in a frame
 * and asks it to go places (a task that needs the person, found in the shell's own "My Work"). The shell may only send
 * a place inside this page, a fragment such as #/p/<id>/task/<id>; anything else is ignored. The theme is chosen in the
 * window's sidebar and told to the page. Outside the app, or in the app's old single-window mode, nothing here does anything.
 */

import { isEmbedded, onNavigate, onTheme, reportFrame } from '../../../internal/nativebridge/bridge.js';
import { theme } from './theme.svelte';

/** Follows the shell's requests to go to a place in this page. Returns a function that stops following. */
export function followShell(): () => void {
  const stopReporting = reportFrame(() => location.hash || '#/');
  const stopNavigation = onNavigate((href: string) => {
    if (href.startsWith('#')) location.hash = href;
  });
  const stopTheme = onTheme((choice: 'light' | 'dark') => theme.set(choice));
  return () => { stopReporting(); stopNavigation(); stopTheme(); };
}

/** Whether a desktop shell frames this page. */
export const embedded = (): boolean => isEmbedded();
