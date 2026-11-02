import type { Agent, Run } from './types';

/**
 * Continuing work is one action: "Continue" starts a new session from what the run recorded, with the agent you pick. This is what a
 * stopped or stuck run can honestly offer, and what stands in the way. Nothing is dispatched until the person approves.
 */
export interface Continuity {
  /** The run needs a decision about whether to carry on, here or elsewhere. */
  needsReview: boolean;
  /** The previous process is confirmed gone. Unknown is not stopped: `recovery: intervention`, an unreachable runner. */
  stopped: boolean;
  /** The session is still open and idle (a limit ended its turn); approving ends it first, in the same step. */
  stopFirst: boolean;
  /** The failure is the provider's limit, so the same agent would only meet it again. */
  providerLimit: boolean;
  headline: string;
  /** Why Approve is unavailable, or empty when it is. */
  hold: string;
}

const KIND: Record<string, string> = {
  rate_limit: 'rate limited',
  quota_exhausted: 'out of allowance',
  context_exhausted: 'out of context',
  authentication: 'signed out',
  other: 'stopped on an error',
};

/** `reachable` is false when the run's runner cannot be reached now (see runReportStale). */
export function continuityOf(run: Run, reachable = true): Continuity {
  const provider = run.blocker?.providerKind ? KIND[run.blocker.providerKind] : '';
  const providerLimit = run.blocker?.providerKind === 'rate_limit' || run.blocker?.providerKind === 'quota_exhausted';
  const none = { needsReview: false, stopped: false, stopFirst: false, providerLimit: false, headline: '', hold: '' };
  if (run.recovery === 'intervention') {
    return { ...none, needsReview: true, headline: 'Interrupted. It is not known whether the process stopped.', hold: 'Confirm on the runner that the previous run has stopped before continuing elsewhere.' };
  }
  if (run.state === 'blocked') {
    const local = !run.remote && reachable;
    return {
      ...none, needsReview: true, providerLimit, stopFirst: local,
      headline: provider ? `Blocked: the provider is ${provider}.` : 'Blocked.',
      hold: local ? '' : 'The runner cannot be reached, so the open session cannot be ended from here.',
    };
  }
  if (run.state === 'failed' || run.state === 'stopped') {
    if (!provider && !run.blocker && run.state === 'stopped') return none;
    const stopped = reachable;
    return { ...none, needsReview: true, stopped, providerLimit, headline: provider ? `Stopped: the provider is ${provider}.` : run.state === 'failed' ? 'The run failed.' : 'The run was stopped.', hold: stopped ? '' : 'The runner cannot be reached, so it is not confirmed that the run stopped.' };
  }
  return none;
}

/** Other agents that could take the work, usable now. */
export function otherProviders(agents: readonly Agent[], current: string): Agent[] {
  return agents.filter(a => a.available && a.id !== current);
}

/** Who may continue: others first; the current agent too, unless its provider's limit is what stopped the work. */
export function continueAgents(agents: readonly Agent[], current: string, providerLimit: boolean): Agent[] {
  const others = otherProviders(agents, current);
  const same = agents.filter(a => a.available && a.id === current);
  return providerLimit ? others : [...others, ...same];
}

/** Approve is allowed once the previous run is ended (or can be ended by this step) and an agent is chosen. */
export function approveHold(c: Continuity, provider: string): string {
  if (!provider) return 'No other agent is available to take this.';
  if (!c.stopped && !c.stopFirst) return c.hold || 'The previous run is not confirmed stopped.';
  return '';
}
