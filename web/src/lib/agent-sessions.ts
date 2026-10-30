// Every coding-agent execution has one identity wherever it is shown: its session key, "run:<id>" for a task run or
// "terminal:<id>" for a native agent terminal. The controller derives the key from the owning record's own ID
// (internal/domain/agentsession.go), the list below is assembled from those records on each read, and nothing here stores a
// second copy of a session. Salie, Agents and the Work pages all open a session by its key; none of them creates one by
// looking.

import { request } from './api';
import type { TerminalSession } from './assistant/terminal';

export type SessionKind = 'run' | 'terminal';

export interface AgentSession {
  key: string;
  kind: SessionKind;
  id: string;
  projectId: string;
  taskId?: string;
  planIds?: string[];
  agentId: string;
  title: string;
  /** The owner's own state word: a run state, or running/exited/stopped/interrupted for a terminal. */
  state: string;
  live: boolean;
  waiting?: boolean;
  device?: string;
  repository?: string;
  /** The Salie conversation that started it, when one did. */
  launchedBy?: string;
  capabilities: { feed: boolean; message: boolean; terminal: boolean; questions: boolean; stop: boolean };
  createdAt: string;
  updatedAt: string;
}

export interface AgentSessionList {
  sessions: AgentSession[];
  /** False when native terminals cannot be listed to this connection, so "none" and "not available here" differ. */
  terminals: boolean;
}

const KEY = /^(run|terminal):([A-Za-z0-9_-]{1,96})$/;

/** Whether a value is exactly a session key. Anything else is never put in an address or a message. */
export const isSessionKey = (value: unknown): value is string => typeof value === 'string' && KEY.test(value);

export function parseSessionKey(key: string): { kind: SessionKind; id: string } | null {
  const m = KEY.exec(key);
  return m ? { kind: m[1] as SessionKind, id: m[2] } : null;
}

export const runKey = (runId: string) => `run:${runId}`;
export const terminalKey = (id: string) => `terminal:${id}`;

export interface SessionFilter { project?: string; task?: string; live?: boolean }

export const agentSessionsApi = {
  list(filter: SessionFilter = {}): Promise<AgentSessionList> {
    const q = new URLSearchParams();
    if (filter.project) q.set('project', filter.project);
    if (filter.task) q.set('task', filter.task);
    if (filter.live) q.set('live', '1');
    const query = q.toString();
    return request<AgentSessionList>('GET', `/api/agent-sessions${query ? `?${query}` : ''}`);
  },
  /** Links a native terminal to a task, or unlinks it with an empty task. The process is not touched. */
  associate: (projectId: string, terminalId: string, taskId: string) =>
    request('PUT', `/api/projects/${encodeURIComponent(projectId)}/terminals/${encodeURIComponent(terminalId)}/task`, { taskId }),
};

/** The terminal renderer's own record of a terminal session: the one terminal implementation is reused as it is. */
export function asTerminal(s: AgentSession): TerminalSession {
  return { id: s.id, projectId: s.projectId, agentId: s.agentId, device: s.device ?? '', repository: s.repository ?? s.title, state: s.state as TerminalSession['state'], createdAt: s.createdAt };
}

/** Live sessions that need the person first, then the other live ones, then the most recent of the rest. */
export function ordered(sessions: AgentSession[]): AgentSession[] {
  return [...sessions].sort((a, b) => Number(b.live) - Number(a.live) || Number(!!b.waiting) - Number(!!a.waiting) || b.updatedAt.localeCompare(a.updatedAt) || a.key.localeCompare(b.key));
}

/** One key, one session: the list a screen shows has no duplicates however it was assembled. */
export function unique(sessions: AgentSession[]): AgentSession[] {
  const seen = new Set<string>();
  return sessions.filter(s => !seen.has(s.key) && !!seen.add(s.key));
}

export function stateLabel(s: Pick<AgentSession, 'kind' | 'state' | 'waiting'>): string {
  if (s.kind === 'terminal') return s.state === 'interrupted' ? 'Interrupted' : s.state === 'running' ? 'Running' : s.state === 'stopped' ? 'Stopped' : 'Ended';
  if (s.waiting) return 'Needs you';
  return s.state.replaceAll('_', ' ').replace(/^./, c => c.toUpperCase());
}
