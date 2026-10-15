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
