// How the shell talks to the pages it shows in frames. A workspace's page cannot call the app; it posts a message to the page
// that framed it, and this decides whether to believe it. The rules are short on purpose: the message must come from the
// window of a frame the shell itself made, from the origin the app said that frame would be at; and it can only ask the app to
// do something (the app then decides, by the kind of workspace the frame is) or say where in itself the person is.

import { isSaliePlacements, type SaliePlacements } from '../lib/salie-placement';

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

export interface MessageLike {
  source: unknown;
  origin: string;
  data: unknown;
}

export interface Deps {
  /** The local Individual page may open Salie with a bounded personal workspace ID. */
  runSession?(projectId: string, runId: string): void;
  personalAssistant?(workspaceId: string, revision?: number): void;
  onboarding?(required: boolean, paused: boolean): void;
  saliePlacement?(value?: SaliePlacements): SaliePlacements;
  relay(id: string, method: string, args: unknown[]): Promise<unknown>;
  remember(id: string, place: string): Promise<void>;
  ready(id: string): void;
  reply(frame: FrameRef, message: Record<string, unknown>): void;
  /** A frame asks to show the person's own Werkbord at a place inside it. */
  open(id: string, target: string, place: string): void;
  /** Individual's assistant says whether it is open and whether something there needs the person. */
  assistant(id: string, state: { open: boolean; waiting: number; running: boolean; compact?: boolean }): void;
  /** A page says the person chose a theme there. */
  theme(id: string, choice: 'light' | 'dark' | ''): void;
  /** A page says the person chose a style there (Paper, Graphite or Classic). */
  style?(id: string, choice: 'paper' | 'graphite' | 'classic'): void;
  /** Individual says something happened that the person asked to hear about while they are in another app. */
  notify(id: string, alert: Alert): void;
  /** A page asks for Settings at a place inside it, rather than showing settings of its own. */
  settings(id: string, place: string): void;
  /** A Team page says the person needs Premium for what they were doing: show Settings at Plan. */
  plan?(id: string): void;
  /**
   * The person's own Werkbord page was refused its sign-in. The app reads the token again and loads the page afresh; the answer is
   * why it could not, in words for the person, or null when it did (the page is then replaced and needs no answer).
   * `manual` is the person pressing "Try again", which is always attempted.
   */
  reauth?(id: string, manual: boolean): Promise<string | null>;
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
    if (frame.surface && m.event !== 'assistant' && m.event !== 'open') return;
    if (m.event === 'salie_placement' && frame.id === 'personal' && !frame.hosted && !frame.surface && d.saliePlacement) {
      if (m.action !== 'get' && m.action !== 'set') return;
      if (m.action === 'set' && !isSaliePlacements(m.value)) return;
      const value = d.saliePlacement(m.action === 'set' ? m.value as SaliePlacements : undefined);
      d.reply(frame, { type: 'werkbord.salie-placement', value });
    }
    else if (m.event === 'onboarding' && frame.id === 'personal' && !frame.hosted && !frame.surface && typeof m.required === 'boolean' && typeof m.paused === 'boolean') d.onboarding?.(m.required, m.paused);
    else if (m.event === 'run_session' && frame.id === 'personal' && !frame.hosted && !frame.surface && typeof m.projectId === 'string' && typeof m.runId === 'string' && /^[\w-]{1,120}$/.test(m.projectId) && /^[\w-]{1,120}$/.test(m.runId)) d.runSession?.(m.projectId, m.runId);
    else if (m.event === 'personal_workspace_assistant' && frame.id === 'personal' && !frame.hosted && !frame.surface && typeof m.workspaceId === 'string' && (m.workspaceId === '' || /^pws_[a-z2-7]{16}$/.test(m.workspaceId))) {
      if (m.revision !== undefined && (!Number.isSafeInteger(m.revision) || (m.revision as number) < 1 || !m.workspaceId)) return;
      if (m.revision === undefined) d.personalAssistant?.(m.workspaceId);
      else d.personalAssistant?.(m.workspaceId, m.revision as number);
    }
    else if (m.event === 'reauth' && frame.id === 'personal' && !frame.surface) {
      const problem = await d.reauth?.(frame.id, m.manual === true);
      if (problem) d.reply(frame, { type: 'werkbord.reauth', ok: false, message: problem });
    }
    else if (m.event === 'ready') d.ready(frame.id);
    // Where a frame that only shows Settings or Help is says nothing about where the person is: it is never remembered.
    else if (m.event === 'place' && isPlace(m.place) && !frame.hosted) await d.remember(frame.id, m.place).catch(() => {});
    else if (m.event === 'theme' && (m.theme === 'light' || m.theme === 'dark' || m.theme === '') && !frame.surface) d.theme(frame.id, m.theme);
    else if (m.event === 'style' && (m.style === 'paper' || m.style === 'graphite' || m.style === 'classic') && !frame.surface) d.style?.(frame.id, m.style);
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
    else if (m.event === 'assistant' && frame.surface && frame.id === 'personal' && typeof m.open === 'boolean' && typeof m.running === 'boolean' && Number.isSafeInteger(m.waiting) && (m.waiting as number) >= 0) {
      d.assistant(frame.id, { open: m.open, waiting: m.waiting as number, running: m.running, ...(m.compact === true ? { compact: true } : {}) });
    }
  }
}

/** Where the assistant's own frame loads from: the person's own Werkbord, as its assistant page and nothing else. */
export function surfaceUrl(workspaceUrl: string): string {
  const u = new URL(workspaceUrl);
  u.searchParams.set('surface', 'assistant');
  return u.toString();
}

/** Where a frame that shows a workspace's page for Settings or Help loads from: the same page, told it is not the workspace on show. */
export function hostedUrl(workspaceUrl: string): string {
  const u = new URL(workspaceUrl);
  u.searchParams.set('hosted', '1');
  return u.toString();
}
