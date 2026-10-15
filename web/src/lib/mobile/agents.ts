// The Agents screen, from what the controller reports about every project's runs and the agents installed on this computer. Only
// reported states are used: a run is what its record says it is, and an agent is "idle" only when it is ready and no run of it is active.
// No framework code, so it can be tested on its own.

import { runStatus } from '../format';
import type { Agent, AttentionRun, Overview } from '../types';

export interface Sessions {
  /** Runs that cannot go on without the person: a question, a block, or a turn that ended. */
  needs: AttentionRun[];
  /** Runs that are working or starting. */
  running: AttentionRun[];
  /** Runs that failed on a task still waiting on them. */
  failed: AttentionRun[];
}

export function agentSessions(overview: Overview | null): Sessions {
  const runs = overview?.runs ?? [];
  return {
    needs: runs.filter((x) => runStatus(x.run).needsInput),
    running: runs.filter((x) => x.run.state === 'running' || x.run.state === 'starting'),
    failed: overview?.failed ?? [],
  };
}

export type FleetState = 'Working' | 'Waiting for you' | 'Idle' | 'Not available';

export interface FleetEntry {
  agent: Agent;
  state: FleetState;
  /** Runs of this agent that are active now. */
  active: number;
}

/** Each installed agent, and what it is doing. An agent that cannot be started says so, and why is in its own guidance. */
export function fleet(agents: readonly Agent[], overview: Overview | null): FleetEntry[] {
  const active = (overview?.runs ?? []).filter((x) => runStatus(x.run).active);
  return agents.map((agent) => {
    const mine = active.filter((x) => x.run.agentId === agent.id);
    const waiting = mine.some((x) => runStatus(x.run).needsInput);
    const state: FleetState = !agent.available && mine.length === 0 ? 'Not available' : waiting ? 'Waiting for you' : mine.length ? 'Working' : 'Idle';
    return { agent, state, active: mine.length };
  });
}

/** "12.3k in · 3.1k out", from a run's reported usage; empty when the agent reported none. */
export function usageLine(run: { usage?: { inputTokens?: number; outputTokens?: number } }): string {
  const u = run.usage;
  if (!u || (u.inputTokens === undefined && u.outputTokens === undefined)) return '';
  const k = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k` : String(n));
  return [u.inputTokens !== undefined ? `${k(u.inputTokens)} in` : '', u.outputTokens !== undefined ? `${k(u.outputTokens)} out` : ''].filter(Boolean).join(' · ');
}

export type AgentFilter = 'all' | 'running' | 'waiting' | 'idle';

export interface AgentCard {
  id: string;
  name: string;
  state: FleetState;
  /** Its sessions that are active now, the ones that need the person first. */
  sessions: AttentionRun[];
  /** What to do when it cannot be started: how to install it or sign in, as the agent says. */
  guidance: string;
}

/**
 * One card for each agent the person has or has a session of: the installed ones, and any agent that appears only in a run (a runner's).
 * Each carries its active sessions, so "what is it doing" is on the card rather than in a separate list.
 */
export function agentCards(agents: readonly Agent[], overview: Overview | null): AgentCard[] {
  const runs = (overview?.runs ?? []).filter((x) => runStatus(x.run).active);
  const known = fleet(agents, overview).map<AgentCard>((e) => ({
    id: e.agent.id,
    name: e.agent.name,
    state: e.state,
    sessions: runs.filter((x) => x.run.agentId === e.agent.id).sort((a, b) => Number(runStatus(b.run).needsInput) - Number(runStatus(a.run).needsInput)),
    guidance: e.agent.available ? '' : (e.agent.guidance ?? e.agent.detail ?? ''),
  }));
  const extra = [...new Set(runs.map((x) => x.run.agentId))]
    .filter((id) => !agents.some((a) => a.id === id))
    .map<AgentCard>((id) => {
      const sessions = runs.filter((x) => x.run.agentId === id);
      return { id, name: id, state: sessions.some((x) => runStatus(x.run).needsInput) ? 'Waiting for you' : 'Working', sessions, guidance: '' };
    });
  return [...known, ...extra].sort((a, b) => rank(a.state) - rank(b.state));
}

const rank = (s: FleetState): number => (s === 'Waiting for you' ? 0 : s === 'Working' ? 1 : s === 'Idle' ? 2 : 3);

export function matchesFilter(c: AgentCard, f: AgentFilter): boolean {
  return f === 'all' || (f === 'running' && c.state === 'Working') || (f === 'waiting' && c.state === 'Waiting for you') || (f === 'idle' && c.state === 'Idle');
}

/** "4 running · 1 waiting for you · 2 idle", only what is true. */
export function fleetLine(cards: readonly AgentCard[]): string {
  const n = (f: AgentFilter) => cards.filter((c) => matchesFilter(c, f)).length;
  return [n('running') ? `${n('running')} running` : '', n('waiting') ? `${n('waiting')} waiting for you` : '', n('idle') ? `${n('idle')} idle` : ''].filter(Boolean).join(' · ') || 'None running';
}
