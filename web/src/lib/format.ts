// Pure helpers that turn run data into what the interface says. No framework
// code here, so it can be tested on its own.

import type { Agent, Run } from './types';

export type Tone = 'work' | 'ask' | 'block' | 'idle' | 'ok' | 'bad' | 'neutral';

export interface RunStatus {
  label: string;
  tone: Tone;
  /** The agent cannot go on without the user. */
  needsInput: boolean;
  /** The run still has a process, or is waiting to continue. */
  active: boolean;
  /** What the user can do about it, in a few words. */
  hint: string;
}

/**
 * How a run is described: one place, so the card, the detail and the control center agree.
 * A task in Doing is, at a glance, one of: Running, Needs input, Blocked, Failed. These
 * are states of the run, not columns of the board.
 */
export function runStatus(run: Run): RunStatus {
  switch (run.state) {
    case 'starting':
      return { label: 'Starting', tone: 'work', needsInput: false, active: true, hint: 'Preparing the agent…' };
    case 'running':
      return { label: 'Running', tone: 'work', needsInput: false, active: true, hint: 'The agent is working.' };
    case 'blocked':
      return {
        label: 'Blocked',
        tone: 'block',
        needsInput: true,
        active: true,
        hint: 'The agent stopped rather than guess. Tell it how to proceed, or stop the run.',
      };
    case 'waiting_for_user':
      return run.waiting === 'question'
        ? { label: 'Needs input', tone: 'ask', needsInput: true, active: true, hint: 'The agent is waiting for your answer.' }
        : { label: 'Waiting for you', tone: 'idle', needsInput: true, active: true, hint: 'The agent finished its turn and is waiting for your next message.' };
    case 'completed':
      return { label: 'Completed', tone: 'ok', needsInput: false, active: false, hint: 'The session is finished.' };
    case 'failed':
      return { label: 'Failed', tone: 'bad', needsInput: false, active: false, hint: run.reason || 'The run failed.' };
    case 'stopped':
      return { label: 'Stopped', tone: 'neutral', needsInput: false, active: false, hint: run.reason || 'The run was stopped.' };
  }
}

/** "3m 12s", "1h 04m", "45s". */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`;
  if (m > 0) return `${m}m ${String(s).padStart(2, '0')}s`;
  return `${s}s`;
}

/** How long a run has lasted: until it ended, or until `now` while it is still going. */
export function runElapsed(run: Run, now: number): string {
  const start = Date.parse(run.createdAt);
  const end = run.endedAt ? Date.parse(run.endedAt) : now;
  return formatDuration(end - start);
}

/** "just now", "5m ago", "3h ago", "2d ago". */
export function timeAgo(iso: string, now: number): string {
  const secs = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  if (secs < 10) return 'just now';
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
  return `${Math.floor(secs / 86400)}d ago`;
}

/** The agent's display name, falling back to its ID. */
export function agentName(agents: Agent[], id: string): string {
  return agents.find((a) => a.id === id)?.name ?? id;
}

/** The line a card shows under the run's status: what it is doing, or why it ended badly. */
export function cardActivity(run: Run): string {
  if (run.state === 'blocked') return run.blocker?.summary ?? '';
  if (run.state === 'failed' || run.state === 'stopped') return run.reason ?? '';
  if (run.state === 'completed') return '';
  return run.activity ?? '';
}

/** Text reduced to one short line, for a card. */
export function oneLine(text: string, max = 160): string {
  const flat = text.replace(/\s+/g, ' ').trim();
  return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
}

/** The current time as the controller writes times. */
export function nowISO(): string {
  return new Date().toISOString();
}
