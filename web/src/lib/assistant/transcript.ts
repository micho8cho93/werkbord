// The visible conversation, built from the assistant's events. It is pure: the store holds one and feeds it events, and the
// tests feed it the same events a server sends. The server keeps no transcript (internal/assistant), so this is what the
// person sees and nothing more; it is rebuilt from the event stream after a reconnect, and kept for the life of the tab.

import type { AssistantEvent, ErrorInfo, PendingAction } from './types';

export interface Step {
  id: string;
  name: string;
  status: 'running' | 'ok' | 'error' | 'waiting' | 'stopped';
  detail?: string;
  code?: string;
}

export interface UserItem {
  kind: 'user';
  id: string;
  text: string;
  files: { name: string; bytes: number }[];
}

export interface ReplyItem {
  kind: 'reply';
  id: string;
  turnId: string;
  text: string;
  /** The provider attempt the text on screen is from; a new one replaces or follows it. */
  attempt: number;
  /** Where in `text` the current attempt begins, so a retry can discard exactly that. */
  attemptStart: number;
  steps: Step[];
  notices: string[];
  status: 'working' | 'done' | 'failed' | 'cancelled';
  error?: ErrorInfo;
}

export type ConfirmState = 'waiting' | 'working' | 'confirmed' | 'declined' | 'expired' | 'failed' | 'gone';

export interface ConfirmItem {
  kind: 'confirm';
  /** The action's id. */
  id: string;
  operation: string;
  summary: string;
  expiresAt: string;
  state: ConfirmState;
  outcome?: string;
}

export interface NoticeItem {
  kind: 'notice';
  id: string;
  text: string;
}

export type Item = UserItem | ReplyItem | ConfirmItem | NoticeItem;

export interface Transcript {
  items: Item[];
  /** The newest event applied. Events at or below it are ignored, so a replay changes nothing. */
  lastSeq: number;
}

export const emptyTranscript = (): Transcript => ({ items: [], lastSeq: 0 });

/** What the server did to the transcript's idea of where it is. */
export type Applied = 'applied' | 'ignored' | 'gap';

const MAX_ITEMS = 80;
const MISSING_END = 'Werkbord reports that this reply has ended, but its final result could not be recovered. Check the board before retrying any changes.';

function replyOf(t: Transcript, turnId: string | undefined): ReplyItem | undefined {
  if (!turnId) return undefined;
  for (let i = t.items.length - 1; i >= 0; i--) {
    const it = t.items[i];
    if (it.kind === 'reply' && it.turnId === turnId) return it;
  }
  return undefined;
}

function ensureReply(t: Transcript, turnId: string | undefined): ReplyItem | undefined {
  if (!turnId) return undefined;
  let r = replyOf(t, turnId);
  if (!r) {
    // The start of this turn was not seen (a page that came back mid-reply, a replay that began later).
    r = { kind: 'reply', id: turnId, turnId, text: '', attempt: 0, attemptStart: 0, steps: [], notices: [], status: 'working' };
    t.items.push(r);
  }
  return r;
}

/** The active reply, if a turn is in progress. */
export function activeReply(t: Transcript): ReplyItem | undefined {
  for (let i = t.items.length - 1; i >= 0; i--) {
    const it = t.items[i];
    if (it.kind === 'reply') return it.status === 'working' ? it : undefined;
  }
  return undefined;
}

/** Applies one event. Mutates `t`. */
export function apply(t: Transcript, e: AssistantEvent): Applied {
  if (e.type === 'gap') return 'gap';
  if (e.seq <= t.lastSeq) return 'ignored';
  t.lastSeq = e.seq;
  switch (e.type) {
    case 'turn_started': {
      const r = ensureReply(t, e.turnId);
      if (r) r.status = 'working';
      break;
    }
    case 'delta': {
      const r = ensureReply(t, e.turnId);
      if (!r || !e.text) break;
      const attempt = e.attempt ?? r.attempt;
      if (attempt !== r.attempt) {
        // The next trip to the provider within the message (after an operation ran): its words follow the earlier ones.
        if (r.text.trim() !== '') r.text += '\n\n';
        r.attemptStart = r.text.length;
        r.attempt = attempt;
      }
      r.text += e.text;
      break;
    }
    case 'retry': {
      // An attempt failed and another begins: what the failed one said is discarded.
      const r = ensureReply(t, e.turnId);
      if (!r) break;
      r.text = r.text.slice(0, r.attemptStart);
      if (e.attempt !== undefined) r.attempt = e.attempt;
      break;
    }
    case 'tool_call': {
      const r = ensureReply(t, e.turnId);
      if (r && e.tool) r.steps.push({ id: e.tool.callId, name: e.tool.name, status: 'running', detail: e.tool.detail });
      break;
    }
    case 'tool_result': {
      const r = ensureReply(t, e.turnId);
      if (!r || !e.tool) break;
      let s = r.steps.find((x) => x.id === e.tool!.callId && x.status === 'running');
      if (!s) {
        s = { id: e.tool.callId, name: e.tool.name, status: 'running' };
        r.steps.push(s);
      }
      s.status = e.tool.status === 'error' ? 'error' : e.tool.status === 'pending_confirmation' ? 'waiting' : 'ok';
      s.code = e.tool.code;
      if (e.tool.detail) s.detail = e.tool.detail;
      break;
    }
    case 'confirmation_required': {
      const p = e.proposal;
      if (!p || t.items.some((i) => i.kind === 'confirm' && i.id === p.actionId)) break;
      t.items.push({ kind: 'confirm', id: p.actionId, operation: p.operation, summary: p.summary, expiresAt: p.expiresAt, state: 'waiting' });
      break;
    }
    case 'confirmation_resolved': {
      const c = t.items.find((i): i is ConfirmItem => i.kind === 'confirm' && i.id === e.actionId);
      if (!c) break;
      const word = e.text;
      c.state = word === 'confirmed' ? 'confirmed' : word === 'declined' ? 'declined' : word === 'expired' ? 'expired' : 'failed';
      if (e.outcome) c.outcome = e.outcome;
      break;
    }
    case 'notice': {
      const r = replyOf(t, e.turnId);
      if (r && e.text && !r.notices.includes(e.text)) r.notices.push(e.text);
      else if (e.text) t.items.push({ kind: 'notice', id: `n${e.seq}`, text: e.text });
      break;
    }
    case 'turn_completed':
    case 'turn_failed':
    case 'turn_cancelled': {
      const r = ensureReply(t, e.turnId);
      if (!r) break;
      r.status = e.type === 'turn_completed' ? 'done' : e.type === 'turn_failed' ? 'failed' : 'cancelled';
      r.error = e.type === 'turn_failed' ? e.error ?? { code: 'internal', message: 'The assistant could not finish.', retryable: true } : undefined;
      r.notices = r.notices.filter((n) => n !== MISSING_END);
      for (const s of r.steps) if (s.status === 'running') s.status = r.status === 'done' ? 'ok' : 'stopped';
      break;
    }
  }
  trim(t);
  return 'applied';
}

function trim(t: Transcript): void {
  if (t.items.length > MAX_ITEMS) t.items.splice(0, t.items.length - MAX_ITEMS);
}

/**
 * A client that comes back (a reload, a reconnect after a restart) knows what the server says is waiting. Proposals the
 * transcript does not have are added, and ones it thinks are waiting that the server no longer lists are closed: they were
 * answered or timed out while the person was away.
 */
export function adoptPending(t: Transcript, pending: PendingAction[]): void {
  const ids = new Set(pending.map((p) => p.id));
  for (const it of t.items) {
    if (it.kind === 'confirm' && (it.state === 'waiting' || it.state === 'working') && !ids.has(it.id)) it.state = 'gone';
  }
  for (const p of pending) {
    const have = t.items.find((i): i is ConfirmItem => i.kind === 'confirm' && i.id === p.id);
    if (have) {
      if (have.state === 'gone' || have.state === 'working') have.state = 'waiting';
    } else t.items.push({ kind: 'confirm', id: p.id, operation: p.operation, summary: p.summary, expiresAt: p.expiresAt, state: 'waiting' });
  }
}

/** Events are numbered per server run: a server that restarted counts from the start again, and the client must too. */
export function reconcileSeq(t: Transcript, serverLast: number): boolean {
  if (serverLast < t.lastSeq) {
    t.lastSeq = serverLast;
    return true;
  }
  return false;
}

/** A turn that was in progress when the page went away but is not any more, as far as the server says. */
export function settleStale(t: Transcript, running: boolean, lastError?: string): void {
  if (running) return;
  for (const it of t.items) {
    if (it.kind === 'reply' && it.status === 'working') {
      it.status = lastError ? 'failed' : 'cancelled';
      if (lastError) it.error = { code: lastError, message: MISSING_END, retryable: true };
      else if (!it.notices.includes(MISSING_END)) it.notices.push(MISSING_END);
      for (const s of it.steps) if (s.status === 'running') s.status = 'stopped';
    }
  }
}

// ---- what the person is told while it works ----

const LABELS: Record<string, string> = {
  get_overview: 'Looking over your projects',
  list_projects: 'Looking at your projects',
  list_labels: 'Checking labels',
  list_tickets: 'Reading the board',
  get_ticket: 'Reading a task',
  get_schedule: 'Checking the schedule',
  list_blockers: 'Looking for what is blocked',
  list_agent_questions: 'Checking what agents are asking',
  list_runs: 'Checking runs',
  get_run_status: 'Checking a run',
  create_ticket: 'Preparing a new task',
  update_ticket: 'Preparing a change',
  answer_question: 'Preparing an answer',
};

/** A plain-language account of an operation, for a person who has never seen its name. */
export function stepLabel(name: string): string {
  return LABELS[name] ?? 'Looking something up';
}

/** The one quiet line shown while the assistant works. */
export function progressLine(r: ReplyItem): string {
  if (r.status !== 'working') return '';
  const running = [...r.steps].reverse().find((s) => s.status === 'running');
  if (running) return stepLabel(running.name) + '…';
  if (r.text.trim() === '') return 'Working…';
  return 'Working…';
}

/** What the finished reply looked at, for the details shown on request: each kind once. */
export function lookedAt(r: ReplyItem): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const s of r.steps) {
    const label = stepLabel(s.name);
    if (!seen.has(label)) {
      seen.add(label);
      out.push(label + (s.status === 'error' ? ' (did not work)' : ''));
    }
  }
  return out;
}

// ---- keeping it for the life of the tab ----

export interface Saved {
  v: 1;
  sessionId: string;
  lastSeq: number;
  items: Item[];
}

const MAX_SAVED_CHARS = 120_000;

/** What is kept in the tab (sessionStorage, which goes with the tab): the visible conversation, capped. No file contents, ever. */
export function serialize(sessionId: string, t: Transcript): string {
  let items = t.items.slice(-40);
  let out = JSON.stringify({ v: 1, sessionId, lastSeq: t.lastSeq, items } satisfies Saved);
  while (out.length > MAX_SAVED_CHARS && items.length > 1) {
    items = items.slice(Math.ceil(items.length / 4));
    out = JSON.stringify({ v: 1, sessionId, lastSeq: t.lastSeq, items } satisfies Saved);
  }
  return out;
}

/** Reads what serialize wrote. Anything that does not look exactly right is dropped: the page then starts clean. */
export function restore(raw: string | null | undefined): Saved | null {
  if (!raw) return null;
  try {
    const s = JSON.parse(raw) as Partial<Saved>;
    if (s.v !== 1 || typeof s.sessionId !== 'string' || !/^ast_[A-Za-z0-9]+$/.test(s.sessionId) || typeof s.lastSeq !== 'number' || !Array.isArray(s.items)) return null;
    const items: Item[] = [];
    for (const it of s.items as unknown[]) {
      const x = it as Record<string, unknown>;
      if (!x || typeof x.id !== 'string') continue;
      if (x.kind === 'user' && typeof x.text === 'string' && Array.isArray(x.files)) {
        items.push({ kind: 'user', id: x.id, text: x.text, files: (x.files as { name?: unknown; bytes?: unknown }[]).filter((f) => typeof f.name === 'string').map((f) => ({ name: String(f.name), bytes: Number(f.bytes) || 0 })) });
      } else if (x.kind === 'reply' && typeof x.text === 'string' && typeof x.turnId === 'string') {
        const r = x as unknown as ReplyItem;
        items.push({ kind: 'reply', id: r.id, turnId: r.turnId, text: r.text, attempt: Number(r.attempt) || 0, attemptStart: Number(r.attemptStart) || 0,
          steps: Array.isArray(r.steps) ? r.steps.filter((st) => st && typeof st.name === 'string').slice(0, 30) : [], notices: Array.isArray(r.notices) ? r.notices.filter((n) => typeof n === 'string') : [],
          status: ['working', 'done', 'failed', 'cancelled'].includes(r.status) ? r.status : 'done', error: r.error });
      } else if (x.kind === 'confirm' && typeof x.summary === 'string') {
        const c = x as unknown as ConfirmItem;
        items.push({ kind: 'confirm', id: c.id, operation: String(c.operation ?? ''), summary: c.summary, expiresAt: String(c.expiresAt ?? ''), state: c.state ?? 'gone', outcome: c.outcome });
      } else if (x.kind === 'notice' && typeof x.text === 'string') items.push({ kind: 'notice', id: x.id, text: x.text });
    }
    return { v: 1, sessionId: s.sessionId, lastSeq: s.lastSeq, items };
  } catch {
    return null;
  }
}
