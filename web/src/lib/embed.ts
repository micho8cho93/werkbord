/**
 * Inside the Werkbord desktop app this web app can be one workspace among several: the app's shell shows it in a frame
 * and asks it to go places (a task that needs the person, found in the shell's own "My Work"). The shell may only send
 * a place inside this page, a fragment such as #/p/<id>/task/<id>; anything else is ignored. The theme is chosen in the
 * window's sidebar and told to the page. Outside the app, or in the app's old single-window mode, nothing here does anything.
 */

import { isEmbedded, onNavigate, onTheme, reportFrame } from '../../../internal/nativebridge/bridge.js';
import { theme } from './theme.svelte';
import { followPlanning } from './planningBridge';

/** Follows the shell's requests to go to a place in this page. Returns a function that stops following. */
export function followShell(): () => void {
  const stopPlanning = followPlanning();
  const stopReporting = reportFrame(() => location.hash || '#/');
  const stopNavigation = onNavigate((href: string) => {
    if (href.startsWith('#')) location.hash = href;
  });
  const stopTheme = onTheme((choice: 'light' | 'dark') => theme.set(choice));
  return () => { stopPlanning(); stopReporting(); stopNavigation(); stopTheme(); };
}

/** Whether a desktop shell frames this page. */
export const embedded = (): boolean => isEmbedded();

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
// else crosses: no conversation, no text, no token.

export interface AssistantRequest {
  action: 'open' | 'close' | 'toggle';
  /** Where the button is, in this page's coordinates. */
  origin?: { x: number; y: number };
}

const finite = (n: unknown): n is number => typeof n === 'number' && Number.isFinite(n) && Math.abs(n) < 100000;

/** Parses a message from the shell. Only the parent window is believed, and only what is exactly this shape. */
export function parseAssistantRequest(data: unknown): AssistantRequest | null {
  const m = data as { type?: unknown; action?: unknown; origin?: { x?: unknown; y?: unknown } } | null;
  if (!m || m.type !== 'werkbord.assistant') return null;
  if (m.action !== 'open' && m.action !== 'close' && m.action !== 'toggle') return null;
  const o = m.origin;
  return { action: m.action, origin: o && finite(o.x) && finite(o.y) ? { x: o.x, y: o.y } : undefined };
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
  window.parent.postMessage({ type: 'werkbord.frame', event: 'assistant', open: r.open, waiting: Math.max(0, Math.min(99, Math.floor(r.waiting))), running: r.running }, '*');
}
