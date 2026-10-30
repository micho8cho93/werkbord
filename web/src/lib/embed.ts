/**
 * Inside the Werkbord desktop app this web app can be one workspace among several: the app's shell shows it in a frame
 * and asks it to go places (a task that needs the person, found in the shell's own "My Work"). The shell may only send
 * a place inside this page, a fragment such as #/p/<id>/task/<id>; anything else is ignored. The theme is chosen in the
 * window's sidebar and told to the page. Outside the app, or in the app's old single-window mode, nothing here does anything.
 */

import { isEmbedded, isWorkspaceHref, onNavigate, onStyle, onTheme, reportFrame } from '../../../internal/nativebridge/bridge.js';
import { look, theme } from './theme.svelte';
import { followPlanning } from './planningBridge';

/** Follows the shell's requests to go to a place in this page. Returns a function that stops following. */
export function followShell(): () => void {
  const stopPlanning = followPlanning();
  const stopReporting = reportFrame(() => location.hash || '#/');
  const stopNavigation = onNavigate((href: string) => {
    if (href.startsWith('#')) location.hash = href;
  });
  const stopTheme = onTheme((choice) => theme.set(choice, true));
  const stopStyle = onStyle((choice) => look.set(choice, true));
  const stopLinks = followShellLinks();
  return () => { stopPlanning(); stopReporting(); stopNavigation(); stopTheme(); stopStyle(); stopLinks(); };
}

/** Whether a desktop shell frames this page. */
export const embedded = (): boolean => isEmbedded();

/**
 * Whether the shell frames this page only to show Settings or Help beside the workspace the person is in (it loads the page with
 * ?hosted=1). Such a page shows the page asked for and nothing of its own around it. Decided once, when the page starts, because
 * adopting the sign-in rewrites the address.
 */
export const hosted: boolean = (() => {
  try {
    return isEmbedded() && new URLSearchParams(location.search).get('hosted') === '1';
  } catch {
    return false;
  }
})();

const SETTINGS_HASH = /^#\/settings(\?|$)/;
const HOSTED_HASH = /^#\/(settings|help)(\?|$)/;

/** Asks the shell to show Settings at a place of this page (`#/settings?phones`): in the window it is one screen, the shell's. */
export function openSettingsInShell(place: string): void {
  if (isEmbedded() && isWorkspaceHref(place)) window.parent.postMessage({ type: 'werkbord.frame', event: 'settings', place }, '*');
}

/** Asks the shell to show a place of the person's own Werkbord (the Workspace page, a project…) as the workspace on show. */
function openInShell(place: string): void {
  if (isWorkspaceHref(place)) window.parent.postMessage({ type: 'werkbord.frame', event: 'open', target: 'personal', place }, '*');
}

/**
 * In the window, a link to this page's settings is a link to the window's Settings: it is asked of the shell and this page stays
 * where it was. A page that only shows Settings or Help does the reverse for everything else: a link from a guide to a project or
 * the Workspace page is the person going there, in the window, not inside the frame that shows Help.
 */
function followShellLinks(): () => void {
  if (!isEmbedded()) return () => {};
  let last = '#/control';
  let armed = false; // a hosted page has been taken to its own place: from then on any other place is the person leaving it
  const check = () => {
    const h = location.hash;
    if (hosted) {
      if (HOSTED_HASH.test(h)) {
        armed = true;
        last = h;
      } else if (armed && h) {
        openInShell(h);
        location.hash = last;
      }
      return;
    }
    if (SETTINGS_HASH.test(h)) {
      openSettingsInShell(h);
      location.hash = last;
    } else if (h) {
      last = h;
    }
  };
  window.addEventListener('hashchange', check);
  check();
  return () => window.removeEventListener('hashchange', check);
}

/**
 * Whether this page is the assistant's own frame in the desktop shell (the shell loads the same app with ?surface=assistant): the
 * assistant over the whole window, and nothing else. It is decided once, when the page starts, because adopting the sign-in rewrites
 * the address.
 */
export const surface: boolean = (() => {
  try {
    return isEmbedded() && new URLSearchParams(location.search).get('surface') === 'assistant';
  } catch {
    return false;
  }
})();

// ---- the assistant, between the page and the window's sidebar ----
//
// The assistant opens out of the button that was pressed. When the window's sidebar holds that button, the button is outside this
// frame: the shell tells the page which way to open and where the button is (in this frame's own coordinates, which may be left of
// its edge), and the page tells the shell whether the assistant is open and whether something there needs the person. Nothing
// else crosses: no conversation text, no token; only the opaque ID of a conversation, so that Agents can lead back to the one that
// started a session, and the key of a session, so that Salie can lead to it.

export interface AssistantRequest {
  action: 'open' | 'close' | 'toggle';
  /** Open this conversation (its ID, nothing of what was said). */
  conversation?: string;
  catchUpPrompt?: string;
  projectContext?: import('./assistant/project-context.svelte').AssistantProjectContext | null;
  /** Where the button is, in this page's coordinates. */
  origin?: { x: number; y: number };
}

const finite = (n: unknown): n is number => typeof n === 'number' && Number.isFinite(n) && Math.abs(n) < 100000;

/** Parses a message from the shell. Only the parent window is believed, and only what is exactly this shape. */
export function parseAssistantRequest(data: unknown): AssistantRequest | null {
  const m = data as { type?: unknown; action?: unknown; conversation?: unknown; catchUpPrompt?: unknown; projectContext?: unknown; origin?: { x?: unknown; y?: unknown } } | null;
  if (!m || m.type !== 'werkbord.assistant') return null;
  if (Object.keys(m).some(key => !['type', 'action', 'conversation', 'catchUpPrompt', 'projectContext', 'origin'].includes(key))) return null;
  if (m.action !== 'open' && m.action !== 'close' && m.action !== 'toggle') return null;
  if (m.conversation !== undefined && (m.action !== 'open' || typeof m.conversation !== 'string' || !/^[\w-]{1,80}$/.test(m.conversation))) return null;
  if (m.catchUpPrompt !== undefined && (m.action !== 'open' || typeof m.catchUpPrompt !== 'string' || m.catchUpPrompt.length > 24000 || m.conversation !== undefined)) return null;
  if (m.projectContext !== undefined && m.projectContext !== null) {
    if (typeof m.projectContext !== 'object' || Array.isArray(m.projectContext)) return null;
    const c = m.projectContext as Record<string, unknown>;
    if (Object.keys(c).length !== 3 || !['workspace', 'project', 'label'].every(k => typeof c[k] === 'string' && (c[k] as string).length > 0 && (c[k] as string).length <= 500)) return null;
  }
  const o = m.origin;
  return { ...(m.projectContext === undefined ? {} : { projectContext: m.projectContext as AssistantRequest['projectContext'] }), ...(m.catchUpPrompt === undefined ? {} : {catchUpPrompt: m.catchUpPrompt as string}), ...(m.conversation ? {conversation: m.conversation as string} : {}), action: m.action, origin: o && finite(o.x) && finite(o.y) ? { x: o.x, y: o.y } : undefined };
}

/** Calls handler when the shell that framed this page asks the assistant to open or close. */
export function onAssistantRequest(handler: (r: AssistantRequest) => void): () => void {
  if (!isEmbedded()) return () => {};
  const listener = (event: MessageEvent) => {
    if (event.source !== window.parent) return;
    const r = parseAssistantRequest(event.data);
    if (r) handler(r);
  };
  window.addEventListener('message', listener);
  return () => window.removeEventListener('message', listener);
}

export interface AssistantReport {
  open: boolean;
  waiting: number;
  running: boolean;
}

/** Tells the shell how the assistant stands, so its sidebar button can show it. Does nothing outside the desktop app. */
export function reportAssistant(r: AssistantReport): void {
  if (!isEmbedded()) return;
  window.parent.postMessage({ type: 'werkbord.frame', event: 'assistant', open: r.open, waiting: Math.max(0, Math.floor(r.waiting)), running: r.running }, '*');
}
