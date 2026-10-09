/**
 * Inside the Werkbord desktop app this web app can be one workspace among several: the app's shell shows it in a frame
 * and asks it to go places (a task that needs the person, found in the shell's own "My Work"). The shell may only send
 * a place inside this page, a fragment such as #/p/<id>/task/<id>; anything else is ignored. Outside the app, or in the
 * app's old single-window mode, nothing here does anything.
 */

import { isEmbedded, onNavigate } from '../../../internal/nativebridge/bridge.js';

/** Follows the shell's requests to go to a place in this page. Returns a function that stops following. */
export function followShell(): () => void {
  return onNavigate((href: string) => {
    if (href.startsWith('#')) location.hash = href;
  });
}

/** Whether a desktop shell frames this page. */
export const embedded = (): boolean => isEmbedded();
