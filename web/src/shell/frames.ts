// How the shell talks to the pages it shows in frames. A workspace's page cannot call the app; it posts a message to the page
// that framed it, and this decides whether to believe it. The rules are short on purpose: the message must come from the
// window of a frame the shell itself made, from the origin the app said that frame would be at; and it can only ask the app to
// do something (the app then decides, by the kind of workspace the frame is) or say where in itself the person is.

import { parseChain } from '../lib/origin';
import { REPO_NAME } from '../lib/repowatch';

export interface FrameRef {
  /** The workspace the frame shows: the app's identity for it, never anything the page says. */
  id: string;
  /** The frame is the assistant's own, not a workspace's page: it may ask the app for things and say how the assistant stands, and nothing else. */
  surface?: boolean;
  /** The frame shows a workspace's page for Settings or Help, beside the one the person is in: where it is says nothing about where they are. */
  hosted?: boolean;
  /** The origin the app said the frame is loaded from. */
  origin: string;
  window: Window | null;
}

/** What a session key looks like (agent-sessions.ts): the kind of record that owns it, and that record's own ID. */
const SESSION_KEY = /^(run|terminal):[A-Za-z0-9_-]{1,96}$/;

export interface MessageLike {
  source: unknown;
  origin: string;
  data: unknown;
}

export interface Deps {
  /** A page inside a workspace's frame was asked to jump somewhere (⌘K): the window's one palette answers, not a second one in the frame. */
  palette?(): void;
  /** A page in the frame was asked to go back (Escape on Help): the window answers by leaving Settings or Help for where they were opened from. */
  leave?(): void;
  workSalie?(workspace: string, project: string, plan: string, ask?: 'chain'): void;
  /** A page of the person's own Werkbord, or Salie, asks to open a coding-agent session in Agents. The key is a validated session key. */
  agentSession?(key?: string, from?: string): void;
  /** A page of the person's own Werkbord goes back to one of the window's own pages (Work, Calendar), as it was left. */
  page?(page: 'inbox' | 'calendar' | 'overview' | 'reported'): void;
  /** Agents asks to go back to Salie, to the conversation that started the session when it names one. */
  openSalie?(conversation?: string): void;
  onboarding?(required: boolean, paused: boolean): void;
  /** The person's own Werkbord says where each branch of a team's repository is on GitHub. */
  repoHeads?(repo: string, heads: { name: string; sha: string }[]): void;
  /** The person's own Werkbord saw a team's repository change on GitHub. */
  repoChange?(change: RepoChange): void;
  relay(id: string, method: string, args: unknown[]): Promise<unknown>;
  remember(id: string, place: string): Promise<void>;
  ready(id: string): void;
  reply(frame: FrameRef, message: Record<string, unknown>): void;
  /** A frame asks to show the person's own Werkbord at a place inside it. */
  open(id: string, target: string, place: string): void;
  /** Individual's assistant says whether it is open and whether something there needs the person. */
  assistant(id: string, state: { open: boolean; waiting: number; running: boolean }): void;
  /** The brief's frame says how it stands and how tall it is. */
  brief?(state: 'idle' | 'working' | 'done' | 'failed', height: number): void;
  /** A page says the person chose a theme there. */
  theme(id: string, choice: 'light' | 'dark' | ''): void;
  /** A page says the person chose a style there (Paper, Graphite, Classic or Cloud). */
  style?(id: string, choice: 'paper' | 'graphite' | 'classic' | 'cloud' | 'moss' | 'dune' | 'plum'): void;
  /** Individual says something happened that the person asked to hear about while they are in another app. */
  notify(id: string, alert: Alert): void;
  /** A page asks for Settings at a place inside it, rather than showing settings of its own. */
  settings(id: string, place: string): void;
  /** A Team page says the person needs Premium for what they were doing: show Settings at Plan. */
  plan?(id: string): void;
  /** Open map sharing for a project reported by an authenticated Team frame. */
  /**
   * The person's own Werkbord page was refused its sign-in. The app reads the token again and loads the page afresh; the answer is
   * why it could not, in words for the person, or null when it did (the page is then replaced and needs no answer).
   * `manual` is the person pressing "Try again", which is always attempted.
   */
  reauth?(id: string, manual: boolean): Promise<string | null>;
}

/** A push to a team's repository, as the person's own Werkbord saw it on GitHub. */
export interface RepoChange {
  repo: string;
  branch: string;
  sha: string;
  previous?: string;
  created?: boolean;
  authors?: string[];
  count?: number;
  isDefault?: boolean;
  mine?: boolean;
}

export interface Alert {
  /** What happened, once: a repeat of the same key is not told again. */
  key: string;
  title: string;
  body: string;
  /** Something is waiting for the person (a question): the app asks harder for their attention. */
  urgent?: boolean;
}

const KEY = /^[A-Za-z0-9_.:-]{1,64}$/;
const REPO = REPO_NAME;
/** Text for a system notification: plain, short, with nothing that could be taken for anything else. */
function plain(v: unknown, max: number): string | undefined {
  if (typeof v !== 'string') return undefined;
  // eslint-disable-next-line no-control-regex
  const t = v.replace(/[\s\u0000-\u001f\u007f]+/g, ' ').trim();
  return t && t.length <= max ? t : undefined;
}

const METHOD = /^[A-Za-z]{1,40}$/;
const REQUEST_ID = /^[A-Za-z0-9_-]{1,64}$/;
/** A place inside a workspace's own page: the same rule the app applies (internal/workspace ValidHref). */
export const PLACE = /^[#?][A-Za-z0-9/_.:=&%?#-]{0,299}$/;

export function isPlace(s: unknown): s is string {
  if (typeof s !== 'string' || !PLACE.test(s) || s.includes('//') || s.includes('..')) return false;
  try {
    return !/[?&](token|join|invite)=/i.test(decodeURIComponent(s));
  } catch { return false; }
}

/** The frame a message came from, if it is one of ours and said so from the right origin. */
export function frameOf(frames: readonly FrameRef[], e: MessageLike): FrameRef | undefined {
  if (!e.source) return undefined;
  const f = frames.find((x) => x.window !== null && x.window === e.source);
  return f && f.origin === e.origin ? f : undefined;
}

/** Handles one message. Anything that is not exactly what a frame may send is dropped without an answer. */
export async function handle(frames: readonly FrameRef[], e: MessageLike, d: Deps): Promise<void> {
  const frame = frameOf(frames, e);
  const m = e.data as Record<string, unknown> | null;
  if (!frame || !m || typeof m !== 'object') return;
  if (m.type === 'werkbord.native.request') {
    const { id, method, args } = m as { id?: unknown; method?: unknown; args?: unknown };
    const maxArgs = frame.surface && method === 'AssistantWorkspaceCall' ? 6 : 4;
    if (typeof id !== 'string' || !REQUEST_ID.test(id) || typeof method !== 'string' || !METHOD.test(method) || !Array.isArray(args) || args.length > maxArgs) return;
    let size: number;
    try {
      size = JSON.stringify(args).length;
    } catch {
      return;
    }
    const max = frame.surface && method === 'AssistantWorkspaceCall' ? 96 * 1024 : 8192;
    if (size > max) return;
    try {
      const result = await d.relay(frame.id, method, args);
      d.reply(frame, { type: 'werkbord.native.result', id, ok: true, result: result ?? null });
    } catch (err) {
      d.reply(frame, { type: 'werkbord.native.result', id, ok: false, error: err instanceof Error ? err.message : String(err) });
    }
    return;
  }
  if (m.type === 'werkbord.frame') {
    // The assistant's frame is not a workspace's page: it has no place to remember, and may open only a place in the person's own
    // Werkbord or a validated Team ticket route.
    if (frame.surface && m.event !== 'assistant' && m.event !== 'brief' && m.event !== 'open' && m.event !== 'agent_session') return;
    if (m.event === 'onboarding' && frame.id === 'personal' && !frame.hosted && !frame.surface && typeof m.required === 'boolean' && typeof m.paused === 'boolean') d.onboarding?.(m.required, m.paused);
    else if (m.event === 'palette' && !frame.surface) d.palette?.();
    else if (m.event === 'leave' && !frame.surface) d.leave?.();
    else if (m.event === 'work_salie' && !frame.hosted && !frame.surface && typeof m.projectId === 'string' && /^[\w-]{1,120}$/.test(m.projectId) && (m.planId === undefined || typeof m.planId === 'string' && /^[\w-]{1,120}$/.test(m.planId))) d.workSalie?.(frame.id, m.projectId, typeof m.planId === 'string' ? m.planId : '', m.ask === 'chain' ? 'chain' : undefined);
    // A session is opened by its key and nothing else: the key is what the controller derives from the owning record's ID, and the
    // shell shows the person's own Werkbord at it. Only Individual's pages and Salie may ask; a Team page has no session to open.
    // `from` is where the person is coming from (origin.ts): kept only when it is exactly a chain of known origins.
    else if (m.event === 'agent_session' && frame.id === 'personal' && !frame.hosted && (m.key === undefined || SESSION_KEY.test(String(m.key))) && (m.from === undefined || typeof m.from === 'string' && parseChain(m.from).length > 0)) {
      if (m.from === undefined) d.agentSession?.(m.key as string | undefined);
      else d.agentSession?.(m.key as string | undefined, m.from as string);
    }
    else if (m.event === 'page' && !frame.hosted && !frame.surface && (m.page === 'overview' || frame.id === 'personal' && (m.page === 'inbox' || m.page === 'calendar' || m.page === 'reported'))) d.page?.(m.page);
    else if (m.event === 'salie' && frame.id === 'personal' && !frame.hosted && !frame.surface && (m.conversation === undefined || typeof m.conversation === 'string' && /^[\w-]{1,80}$/.test(m.conversation))) d.openSalie?.(typeof m.conversation === 'string' ? m.conversation : undefined);
    else if (m.event === 'reauth' && frame.id === 'personal' && !frame.surface) {
      const problem = await d.reauth?.(frame.id, m.manual === true);
      if (problem) d.reply(frame, { type: 'werkbord.reauth', ok: false, message: problem });
    }
    else if (m.event === 'ready') d.ready(frame.id);
    // Where a frame that only shows Settings or Help is says nothing about where the person is: it is never remembered.
    else if (m.event === 'place' && isPlace(m.place) && !frame.hosted) await d.remember(frame.id, m.place).catch(() => {});
    else if (m.event === 'theme' && (m.theme === 'light' || m.theme === 'dark' || m.theme === '') && !frame.surface) d.theme(frame.id, m.theme);
    else if (m.event === 'style' && (m.style === 'paper' || m.style === 'graphite' || m.style === 'classic' || m.style === 'cloud' || m.style === 'moss' || m.style === 'dune' || m.style === 'plum') && !frame.surface) d.style?.(frame.id, m.style);
    else if (m.event === 'repo_heads' && frame.id === 'personal' && !frame.hosted && !frame.surface && typeof m.repo === 'string' && REPO.test(m.repo) && Array.isArray(m.heads) && m.heads.length > 0 && m.heads.length <= 100) {
      const heads = m.heads.flatMap((h: { name?: unknown; sha?: unknown }) => {
        const name = plain(h?.name, 200);
        return name && typeof h.sha === 'string' && /^[0-9a-f]{40}(?:[0-9a-f]{24})?$/.test(h.sha) ? [{ name, sha: h.sha }] : [];
      });
      if (heads.length) d.repoHeads?.(m.repo, heads);
    }
    else if (m.event === 'repo_change' && frame.id === 'personal' && !frame.hosted && !frame.surface) {
      const repo = typeof m.repo === 'string' && REPO.test(m.repo) ? m.repo : '';
      const sha = (v: unknown) => (typeof v === 'string' && /^[0-9a-f]{7,64}$/.test(v) ? v : undefined);
      const branch = plain(m.branch, 200);
      if (repo && branch && sha(m.sha)) {
        const authors = Array.isArray(m.authors) ? m.authors.map((a) => plain(a, 60)).filter((a): a is string => !!a).slice(0, 3) : undefined;
        d.repoChange?.({ repo, branch, sha: sha(m.sha)!, previous: sha(m.previous), created: m.created === true, authors, count: typeof m.count === 'number' && m.count >= 0 && m.count <= 1000 ? Math.floor(m.count) : undefined, isDefault: m.isDefault === true, mine: m.mine === true });
      }
    }
    else if (m.event === 'notify' && frame.id === 'personal' && !frame.surface && typeof m.key === 'string' && KEY.test(m.key)) {
      const title = plain(m.title, 80);
      const body = plain(m.body, 200);
      if (title && body !== undefined) d.notify(frame.id, { key: m.key, title, body, urgent: m.urgent === true });
    }
    // A Team page that refused a change because the team needs Premium (or is read-only for this person) sends the person to Plan,
    // where the unlock key is entered. It carries nothing: the shell opens its own page.
    else if (m.event === 'plan' && frame.id.startsWith('team:') && !frame.hosted && !frame.surface) d.plan?.(frame.id);
    // Individual's `#/settings…` and Team's `?tab=…` that lead to what Settings lists. Not from a frame that is itself showing Settings.
    else if (m.event === 'settings' && isPlace(m.place) && !frame.hosted && !frame.surface && (frame.id === 'personal' ? String(m.place).startsWith('#/settings') : String(m.place).startsWith('?tab='))) d.settings(frame.id, m.place as string);
    else if (m.event === 'open' && isPlace(m.place) && typeof m.target === 'string' && m.target.startsWith('team:') && frame.surface && frame.id === 'personal' && String(m.place).startsWith('?')) d.open(frame.id, m.target, m.place as string);
    // A place inside the person's own Werkbord, from a Team page, or from the assistant's own frame (which is that Werkbord) when a card
    // there leads to a task, a branch or the Control Deck. Only a `#` place that passes isPlace: never an address, a token or a Team place.
    else if (m.event === 'open' && m.target === 'personal' && isPlace(m.place) && String(m.place).startsWith('#') && (!frame.surface || frame.id === 'personal')) d.open(frame.id, 'personal', m.place as string);
    // Only the assistant's own frame, which is the person's own Werkbord, says how the assistant stands. A count and two flags are
    // all that cross: never a word of the conversation.
    // The brief's frame says only that it is writing, done or failed, and how tall it is: never a word of what it wrote.
    else if (m.event === 'brief' && frame.surface && frame.id === 'personal' && (m.state === 'idle' || m.state === 'working' || m.state === 'done' || m.state === 'failed') && typeof m.height === 'number' && Number.isFinite(m.height) && m.height >= 0 && m.height <= 4000) {
      d.brief?.(m.state, Math.round(m.height));
    }
    else if (m.event === 'assistant' && frame.surface && frame.id === 'personal' && typeof m.open === 'boolean' && typeof m.running === 'boolean' && Number.isSafeInteger(m.waiting) && (m.waiting as number) >= 0) {
      d.assistant(frame.id, { open: m.open, waiting: m.waiting as number, running: m.running });
    }
  }
}

/** Where the assistant's own frame loads from: the person's own Werkbord, as its assistant page and nothing else. */
export function surfaceUrl(workspaceUrl: string, surface: 'assistant' | 'brief' = 'assistant'): string {
  const u = new URL(workspaceUrl);
  u.searchParams.set('surface', surface);
  return u.toString();
}

/** Where a frame that shows a workspace's page for Settings or Help loads from: the same page, told it is not the workspace on show. */
export function hostedUrl(workspaceUrl: string): string {
  const u = new URL(workspaceUrl);
  u.searchParams.set('hosted', '1');
  return u.toString();
}
