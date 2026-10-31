// Moving between Salie and Agents. Both are places for the same sessions, so moving never starts, stops or copies anything: it
// only says which place shows which session, by its key (agent-sessions.ts).

import { embedded } from './embed';
import { isSessionKey } from './agent-sessions';
import { sessionsHref } from './location';
import { encodeChain, withChain, type Origin } from './origin';

/**
 * Opens Agents, with the session of this key open (or the list alone). In the desktop window the window shows its Agents
 * destination; in a plain browser the app's own Agents page is the address. `from` is where the person is coming from (origin.ts),
 * so that Back on the session leads there; a session reached with none goes back to the list.
 */
export function openAgents(key?: string, from: readonly Origin[] = []): void {
  if (key !== undefined && !isSessionKey(key)) return;
  if (embedded()) {
    window.parent.postMessage({ type: 'werkbord.frame', event: 'agent_session', ...(key ? { key } : {}), ...(from.length ? { from: encodeChain(from) } : {}) }, '*');
    return;
  }
  // The router follows the address, so this is the same move as following a link. Salie, if it was open over the page, is put away;
  // whatever it is running goes on.
  location.hash = withChain(sessionsHref(key), from);
  void import('./assistant/sessions.svelte').then(({ assistant }) => assistant.hide());
}

/** Goes back to Salie, to the conversation that started a session when it names one. The session is left running. */
export function backToSalie(conversation?: string): void {
  if (embedded()) {
    window.parent.postMessage({ type: 'werkbord.frame', event: 'salie', ...(conversation ? { conversation } : {}) }, '*');
    return;
  }
  void import('./assistant/sessions.svelte').then(({ assistant }) => {
    if (conversation) assistant.openConversation(conversation);
    else assistant.show();
  });
}
