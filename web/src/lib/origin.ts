// Where a detail page came from, so that Back goes there. Pure, so it can be tested on its own.
//
// An origin is carried in the address (`?from=`), not in browser history: a reload, a pasted link or a window opened fresh
// still know the way back. It is a chain, nearest first, because a detail page is often several steps deep (Calendar → task →
// session): going back pops the nearest and hands the rest on, so each page on the way back still knows where it came from.
//
//   from=work                     the Work page (the desktop window's, or Control in a plain browser)
//   from=calendar[:<project>]     the window's Calendar; with a project, that project's own calendar page
//   from=agents                   the Agents list
//   from=salie[:<conversation>]   Salie, on the conversation when one is named
//   from=board:<project>          a project's Work board
//   from=plan:<project>:<plan>    a Work plan
//   from=task:<project>:<task>    a task
//   from=session:<key>            an agent session (key is run:<id> or terminal:<id>)
//   from=task:p:t>calendar        a chain: a task, which was reached from the Calendar
//
// Nothing here navigates. An origin names a place; the page that shows Back decides how to get there.

export type Origin =
  | { kind: 'work' }
  | { kind: 'agents' }
  | { kind: 'calendar'; projectId?: string }
  | { kind: 'salie'; conversation?: string }
  | { kind: 'board'; projectId: string }
  | { kind: 'plan'; projectId: string; planId: string }
  | { kind: 'task'; projectId: string; taskId: string }
  | { kind: 'session'; key: string };

/** How many steps back an address remembers. Deeper than the longest route there is, short enough for a place to stay short. */
export const MAX_CHAIN = 3;
/** A place longer than this is refused by the window (frames.ts PLACE); the oldest steps are dropped to stay under it. */
const MAX_PLACE = 280;

const ID = /^[A-Za-z0-9_-]{1,96}$/;
const SESSION = /^(run|terminal):[A-Za-z0-9_-]{1,96}$/;
const CONVERSATION = /^[\w-]{1,80}$/;

const part = (s: string): string => encodeURIComponent(s);
const unpart = (s: string): string => {
  try { return decodeURIComponent(s); } catch { return ''; }
};

/** One origin as text. */
export function encodeOrigin(o: Origin): string {
  switch (o.kind) {
    case 'work': case 'agents': return o.kind;
    case 'calendar': return o.projectId ? `calendar:${part(o.projectId)}` : 'calendar';
    case 'salie': return o.conversation ? `salie:${part(o.conversation)}` : 'salie';
    case 'board': return `board:${part(o.projectId)}`;
    case 'plan': return `plan:${part(o.projectId)}:${part(o.planId)}`;
    case 'task': return `task:${part(o.projectId)}:${part(o.taskId)}`;
    case 'session': return `session:${part(o.key)}`;
  }
}

/** One origin from text. Anything that is not exactly one of the shapes above is nothing: an address cannot make Back go anywhere odd. */
export function parseOne(text: string): Origin | null {
  const [kind, a = '', b = '', ...extra] = text.split(':');
  if (extra.length) return null;
  const x = unpart(a);
  const y = unpart(b);
  switch (kind) {
    case 'work': return a || b ? null : { kind: 'work' };
    case 'agents': return a || b ? null : { kind: 'agents' };
    case 'calendar': return b ? null : !a ? { kind: 'calendar' } : ID.test(x) ? { kind: 'calendar', projectId: x } : null;
    case 'salie': return b ? null : !a ? { kind: 'salie' } : CONVERSATION.test(x) ? { kind: 'salie', conversation: x } : null;
    case 'board': return !b && ID.test(x) ? { kind: 'board', projectId: x } : null;
    case 'plan': return ID.test(x) && ID.test(y) ? { kind: 'plan', projectId: x, planId: y } : null;
    case 'task': return ID.test(x) && ID.test(y) ? { kind: 'task', projectId: x, taskId: y } : null;
    case 'session': return !b && SESSION.test(x) ? { kind: 'session', key: x } : null;
    default: return null;
  }
}

/** A chain, nearest first, as text. */
export function encodeChain(chain: readonly Origin[]): string {
  return chain.slice(0, MAX_CHAIN).map(encodeOrigin).join('>');
}

/** A chain from text. A chain with any step that is not understood is no chain: half a route would lead somewhere unexpected. */
export function parseChain(text: string | null | undefined): Origin[] {
  if (!text || text.length > 400) return [];
  const steps = text.split('>');
  if (steps.length > MAX_CHAIN) return [];
  const out: Origin[] = [];
  for (const s of steps) {
    const one = parseOne(s);
    if (!one) return [];
    out.push(one);
  }
  return out;
}

/** The chain an address (or the query after its `?`) carries. */
export function chainOf(hashOrQuery: string): Origin[] {
  const i = hashOrQuery.indexOf('?');
  const query = i === -1 ? (hashOrQuery.startsWith('#') ? '' : hashOrQuery) : hashOrQuery.slice(i + 1);
  return parseChain(new URLSearchParams(query).get('from'));
}

/** The chain for a page reached from `here`: `here` first, then what led to it. */
export function pushed(here: Origin, chain: readonly Origin[]): Origin[] {
  return [here, ...chain].slice(0, MAX_CHAIN);
}

/** The route back from a page whose chain is this: where it goes, and the chain that place is shown with. */
export function popped(chain: readonly Origin[]): { to: Origin; rest: Origin[] } | null {
  return chain.length ? { to: chain[0], rest: chain.slice(1) } : null;
}

/** The address with its chain set (or removed, when the chain is empty). Other parts of the query are kept as they were. */
export function withChain(href: string, chain: readonly Origin[]): string {
  const i = href.indexOf('?');
  const path = i === -1 ? href : href.slice(0, i);
  const params = new URLSearchParams(i === -1 ? '' : href.slice(i + 1));
  params.delete('from');
  let steps = [...chain].slice(0, MAX_CHAIN);
  const build = () => {
    const copy = new URLSearchParams(params);
    if (steps.length) copy.set('from', encodeChain(steps));
    const q = copy.toString();
    return q ? `${path}?${q}` : path;
  };
  let out = build();
  while (steps.length > 1 && out.length > MAX_PLACE) { steps = steps.slice(0, -1); out = build(); }
  return out;
}

/** What the words of a trail are taken from. Anything unknown is named plainly, so the trail never shows an empty link. */
export interface Names {
  task?(projectId: string, taskId: string): string | undefined;
  plan?(projectId: string, planId: string): string | undefined;
}

/** The words for an origin in a trail. */
export function labelOf(o: Origin, names: Names = {}): string {
  switch (o.kind) {
    case 'work': case 'board': return 'Work';
    case 'agents': return 'Agents';
    case 'calendar': return 'Calendar';
    case 'salie': return 'Salie';
    case 'session': return 'Session';
    case 'plan': return names.plan?.(o.projectId, o.planId) || 'Plan';
    case 'task': return names.task?.(o.projectId, o.taskId) || 'Task';
  }
}

/** The pages above a page, oldest first: the whole trail for a breadcrumb, whose last step is where Back goes. */
export function trail(chain: readonly Origin[]): Origin[] {
  return [...chain].reverse();
}
