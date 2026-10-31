// What the Agents overview says about the sessions it lists, and what a person may do to one. Pure: it reads the controller's session
// records (agent-sessions.ts) and decides nothing about them. It never starts, stops or copies a session.

import { stateLabel, type AgentSession } from './agent-sessions';

export type Show = 'all' | 'live' | 'task' | 'standalone';

export interface OverviewFilter { project?: string; provider?: string; show?: Show }

/** How a session that is no longer live ended, in one word. */
export type Outcome = 'completed' | 'failed' | 'interrupted' | 'stopped' | 'ended';

export function outcomeOf(s: Pick<AgentSession, 'kind' | 'state' | 'live'>): Outcome {
  if (s.live) return 'ended';
  if (s.state === 'failed') return 'failed';
  if (s.state === 'interrupted') return 'interrupted';
  if (s.state === 'stopped') return 'stopped';
  if (s.state === 'completed' || s.state === 'exited') return 'completed';
  return 'ended';
}

export const OUTCOME_LABELS: Record<Outcome, string> = { completed: 'Completed', failed: 'Failed', interrupted: 'Interrupted', stopped: 'Stopped', ended: 'Ended' };

export interface Overview {
  /** Live sessions waiting for the person. */
  needsYou: AgentSession[];
  /** Live sessions working on their own. */
  running: AgentSession[];
  /** Sessions that are over, failed and interrupted first, then the newest. */
  recent: AgentSession[];
  counts: { needsYou: number; running: number; failed: number; recent: number };
}

const sinceNewest = (a: AgentSession, b: AgentSession) => b.updatedAt.localeCompare(a.updatedAt) || a.key.localeCompare(b.key);
const trouble = (s: AgentSession) => (['failed', 'interrupted'] as Outcome[]).includes(outcomeOf(s));

/** Whether a session passes the filter: the project, the provider (agent) and what kind of session. */
export function matches(s: AgentSession, f: OverviewFilter): boolean {
  if (f.project && s.projectId !== f.project) return false;
  if (f.provider && s.agentId !== f.provider) return false;
  switch (f.show ?? 'all') {
    case 'live': return s.live;
    case 'task': return !!s.taskId;
    case 'standalone': return !s.taskId;
    default: return true;
  }
}

/** The overview's three groups, from sessions that are already one per key. Only real sessions are here: a task that is merely
 *  scheduled has no session until it starts, and the Work queue is not repeated. */
export function overviewOf(sessions: readonly AgentSession[], f: OverviewFilter = {}): Overview {
  const shown = sessions.filter(s => matches(s, f));
  const needsYou = shown.filter(s => s.live && s.waiting).sort(sinceNewest);
  const running = shown.filter(s => s.live && !s.waiting).sort(sinceNewest);
  const recent = shown.filter(s => !s.live).sort((a, b) => Number(trouble(b)) - Number(trouble(a)) || sinceNewest(a, b));
  return { needsYou, running, recent, counts: { needsYou: needsYou.length, running: running.length, failed: recent.filter(s => outcomeOf(s) === 'failed').length, recent: recent.length } };
}

/** The providers (agents) of the sessions, for the filter, each once. */
export function providersOf(sessions: readonly AgentSession[]): string[] {
  return [...new Set(sessions.map(s => s.agentId))].sort();
}

/** The summary line's parts, only those that say something: "2 need you", "3 running", "1 failed". */
export function summaryOf(o: Overview): { id: 'needsYou' | 'running' | 'failed'; text: string }[] {
  const out: { id: 'needsYou' | 'running' | 'failed'; text: string }[] = [];
  if (o.counts.needsYou) out.push({ id: 'needsYou', text: o.counts.needsYou === 1 ? '1 needs you' : `${o.counts.needsYou} need you` });
  if (o.counts.running) out.push({ id: 'running', text: `${o.counts.running} running` });
  if (o.counts.failed) out.push({ id: 'failed', text: `${o.counts.failed} failed` });
  return out;
}

/** What the person may do to an open session, from what the controller says the session offers and whether it can be reached now.
 *  A control that is not here is not drawn: a session this controller cannot operate is read, not driven. */
export interface Controls {
  /** Instructions and answers to a run. */
  message: boolean;
  /** Stop a run, or end a terminal's process. */
  stop: boolean;
  /** Type into a terminal. */
  terminal: boolean;
  /** Why the controls are not offered, said plainly; empty when they are. */
  reason: string;
}

export function controlsFor(s: Pick<AgentSession, 'live' | 'capabilities'> | undefined, connection: string = 'live'): Controls {
  if (!s) return { message: false, stop: false, terminal: false, reason: '' };
  if (!s.live) return { message: false, stop: false, terminal: false, reason: 'This session has ended. Its activity remains available.' };
  if (connection !== 'live') return { message: false, stop: false, terminal: false, reason: 'Not connected to the controller, so this session cannot be operated right now. What is shown may be out of date.' };
  const c = s.capabilities;
  const message = c.message || c.questions;
  const stop = c.stop;
  const terminal = c.terminal;
  return { message, stop, terminal, reason: message || stop || terminal ? '' : 'This controller can show this session but not operate it.' };
}

/** A session has task context to show only when it belongs to a task: a standalone session gives its space to the conversation. */
export const hasTaskContext = (s: Pick<AgentSession, 'taskId'> | undefined): boolean => !!s?.taskId;

export type Freshness = 'live' | 'recent' | 'stale' | 'unknown';

export interface Reported { label: string; freshness: Freshness }

/** How current an activity report from another computer is, said as it is: when it was recorded, or that it is the last known. */
export function reportedLine(report: { recordedAt?: string; stale: boolean } | undefined, now: number, available = true): Reported {
  if (!available) return { label: 'Unavailable · last known', freshness: 'unknown' };
  const at = report?.recordedAt ? Date.parse(report.recordedAt) : NaN;
  if (!Number.isFinite(at)) return { label: report?.stale ? 'Last reported' : 'Time not reported', freshness: report?.stale ? 'stale' : 'unknown' };
  const secs = Math.max(0, Math.floor((now - at) / 1000));
  const ago = secs < 10 ? 'just now' : secs < 60 ? `${secs}s ago` : secs < 3600 ? `${Math.floor(secs / 60)}m ago` : secs < 86400 ? `${Math.floor(secs / 3600)}h ago` : `${Math.floor(secs / 86400)}d ago`;
  if (report?.stale) return { label: `Last reported ${ago}`, freshness: 'stale' };
  return { label: `Reported ${ago}`, freshness: secs < 120 ? 'live' : 'recent' };
}

/** The state of a session for a line of text. */
export const sessionState = (s: AgentSession): string => (s.live ? stateLabel(s) : OUTCOME_LABELS[outcomeOf(s)]);
