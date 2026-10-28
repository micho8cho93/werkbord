/**
 * Signing in again, inside the desktop window.
 *
 * The desktop app signs a workspace's page in by opening it with the access token in the address. A page that is refused anyway (the
 * controller was replaced under it, the token could not be read at that moment) used to fall back to a prompt for a token the person
 * cannot find. Inside the window it asks the app's shell instead, which reads the token again and loads the page afresh; when that
 * does not help, it says why. Outside the window nothing here does anything: a browser really does need the token typed or linked.
 */
import { isEmbedded } from '../../../internal/nativebridge/bridge.js';

export type SignInState = 'idle' | 'asking' | 'failed';

export const signIn = $state<{ state: SignInState; message: string }>({ state: 'idle', message: '' });

/** The shell answers within its own limit (8 seconds); a page that has heard nothing by then says so. */
const ANSWER_WITHIN = 12000;
let timer: ReturnType<typeof setTimeout> | undefined;
let listening = false;
let askedOnce = false;

/** The shell's words for why it could not sign this page in. Only the parent window is believed, and only plain short text. */
export function parseSignInFailure(data: unknown): string | null {
  const m = data as { type?: unknown; ok?: unknown; message?: unknown } | null;
  if (!m || m.type !== 'werkbord.reauth' || m.ok !== false || typeof m.message !== 'string') return null;
  const text = m.message.replace(/\s+/g, ' ').trim();
  return text && text.length <= 400 ? text : 'The app could not sign this window in.';
}

function listen(): void {
  if (listening) return;
  listening = true;
  window.addEventListener('message', (event) => {
    if (event.source !== window.parent) return;
    const message = parseSignInFailure(event.data);
    if (message === null) return;
    clearTimeout(timer);
    signIn.state = 'failed';
    signIn.message = message;
  });
}

/** The assistant's own frame is not a workspace's page: the shell does not sign it in again, so it does not ask. */
function isAssistantFrame(): boolean {
  try {
    return new URLSearchParams(location.search).get('surface') === 'assistant';
  } catch {
    return false;
  }
}

/** Whether this page is somewhere that can ask the app to sign it in again. */
export const canAskForSignIn = (): boolean => isEmbedded() && !isAssistantFrame();

/**
 * Asks the shell to sign this page in again. By itself (`manual` false) it asks once per page load, so a page that is refused again
 * straight after does not ask in a circle; the person pressing "Try again" (`manual`) always gets an answer. Returns whether it asked.
 */
export function askForSignIn(manual = false): boolean {
  if (!canAskForSignIn() || (!manual && askedOnce)) return false;
  askedOnce = true;
  listen();
  signIn.state = 'asking';
  signIn.message = '';
  clearTimeout(timer);
  // When the shell succeeds it loads this page again, so there is no answer to wait for; silence for this long means it did not hear.
  timer = setTimeout(() => {
    if (signIn.state !== 'asking') return;
    signIn.state = 'failed';
    signIn.message = 'The app did not answer.';
  }, ANSWER_WITHIN);
  window.parent.postMessage({ type: 'werkbord.frame', event: 'reauth', manual }, '*');
  return true;
}
