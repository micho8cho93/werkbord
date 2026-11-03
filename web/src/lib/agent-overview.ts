// What the Agents overview says about the sessions it lists, and what a person may do to one. Pure: it reads the controller's session
// records (agent-sessions.ts) and decides nothing about them. It never starts, stops or copies a session.

import { stateLabel, type AgentSession } from './agent-sessions';
import type { StepAgent } from './work-steps';
import type { WorkPlan } from './work-queue';

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

/** A session that ended badly (failed, or cut off by a restart): it is shown above the ones that finished well, with a way to try again. */
export const isTrouble = (s: Pick<AgentSession, 'kind' | 'state' | 'live'>): boolean => !s.live && (['failed', 'interrupted'] as Outcome[]).includes(outcomeOf(s));

/** Whether a session that is over can be run again from its task: only a task run that did not finish well. A terminal is opened afresh instead. */
export const canRetry = (s: Pick<AgentSession, 'kind' | 'state' | 'live' | 'taskId'>): boolean =>
  s.kind === 'run' && !!s.taskId && !s.live && (['failed', 'interrupted', 'stopped'] as Outcome[]).includes(outcomeOf(s));

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

/** A live native terminal that has had no output and no input for this long is called idle. */
export const TERMINAL_IDLE_MS = 24 * 60 * 60 * 1000;

/** How long a live native terminal has sat untouched, or null when this is not one or it has not been idle for a day. */
export function idleFor(s: Pick<AgentSession, 'kind' | 'live' | 'updatedAt'>, now: number): number | null {
  if (s.kind !== 'terminal' || !s.live) return null;
  const last = Date.parse(s.updatedAt);
  if (!Number.isFinite(last)) return null;
  const ms = now - last;
  return ms >= TERMINAL_IDLE_MS ? ms : null;
}

/** What to say about an idle terminal: how long, and that stopping it is up to the person. Empty when it is not idle. */
export function idleNote(s: Pick<AgentSession, 'kind' | 'live' | 'updatedAt'>, now: number): string {
  const ms = idleFor(s, now);
  if (ms === null) return '';
  const days = Math.floor(ms / 86_400_000);
  return `No activity for ${days === 1 ? '1 day' : `${days} days`}. Stop it if you are done.`;
}

/** The state of a session for a line of text. */
export const sessionState = (s: AgentSession): string => (s.live ? stateLabel(s) : OUTCOME_LABELS[outcomeOf(s)]);

/**
 * A session as the identity layer wants it. `stale` is the caller's word that the controller (or, for a remote run, its runner) can no
 * longer confirm the session: only a confirmed live session turns the ring. A session that was cut off by a restart is not known to have
 * stopped, so it reads as the last thing reported, never as stopped.
 */
export function sessionAgent(s: Pick<AgentSession, 'kind' | 'state' | 'live' | 'waiting' | 'agentId' | 'updatedAt'>, stale: boolean): StepAgent {
  const observedAt = s.updatedAt;
  if (s.live) {
    const state = s.waiting ? 'needs_input' : s.state === 'starting' ? 'starting' : s.state === 'blocked' ? 'blocked' : 'running';
    return { agentId: s.agentId, state, fresh: !stale, observedAt };
  }
  switch (outcomeOf(s)) {
    case 'completed': return { agentId: s.agentId, state: 'completed', fresh: true, observedAt };
    case 'failed': return { agentId: s.agentId, state: 'failed', fresh: true, observedAt };
    case 'interrupted': return { agentId: s.agentId, state: 'running', fresh: false, observedAt };
    default: return { agentId: s.agentId, state: 'stopped', fresh: true, observedAt };
  }
}

/** A run of sessions under one plan, or sessions that belong to no plan (`planId` empty, no sub-header). */
export interface PlanBlock { planId: string; sessions: AgentSession[] }

/**
 * Splits an already-ordered group into blocks, one per plan. A plan's block sits where its first session sits, so the group's order (most
 * urgent first) is never rearranged; sessions with no known plan stay where they are, each block without a header. A session in several
 * plans goes under the first of its plans that is known.
 */
export function planBlocks(sessions: readonly AgentSession[], known: ReadonlySet<string>): PlanBlock[] {
  const blocks: PlanBlock[] = [];
  const byPlan = new Map<string, PlanBlock>();
  for (const s of sessions) {
    const planId = s.planIds?.find(id => known.has(id)) ?? '';
    if (!planId) {
      const last = blocks[blocks.length - 1];
      if (last && !last.planId) last.sessions.push(s);
      else blocks.push({ planId: '', sessions: [s] });
      continue;
    }
    const kept = byPlan.get(planId);
    if (kept) kept.sessions.push(s);
    else { const b = { planId, sessions: [s] }; byPlan.set(planId, b); blocks.push(b); }
  }
  return blocks;
}

/** "Authentication overhaul · 2 of 5 tasks complete". */
export function planHeading(plan: Pick<WorkPlan, 'title'>, progress: { done: number; total: number }): string {
  return `${plan.title} · ${progress.done} of ${progress.total} ${progress.total === 1 ? 'task' : 'tasks'} complete`;
}

/** The first task of the plan after this one that is not complete: what "Next after this" names. Nothing when this is the last open step. */
export function nextAfter(plan: Pick<WorkPlan, 'taskIds'>, taskId: string, isDone: (id: string) => boolean): string | undefined {
  const at = plan.taskIds.indexOf(taskId);
  if (at < 0) return undefined;
  return plan.taskIds.slice(at + 1).find(id => !isDone(id));
}
