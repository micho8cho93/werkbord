import type { Agent, Run } from './types';

/** Provider continuity (assessment only). What a stopped or stuck run can honestly offer, without dispatching anything. */
export interface Continuity {
  /** The run needs a decision about whether to carry on elsewhere. */
  needsReview: boolean;
  /** The previous process is confirmed gone. Unknown is not stopped: `recovery: intervention`, a live blocked session, an unreachable runner. */
  stopped: boolean;
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
  const none = { needsReview: false, stopped: false, headline: '', hold: '' };
  if (run.recovery === 'intervention') {
    return { needsReview: true, stopped: false, headline: 'Interrupted. It is not known whether the process stopped.', hold: 'Confirm on the runner that the previous run has stopped before continuing elsewhere.' };
  }
  if (run.state === 'blocked') {
    return { needsReview: true, stopped: false, headline: provider ? `Blocked: the provider is ${provider}.` : 'Blocked.', hold: 'The session is still open. Stop it, or wait for the provider, before continuing elsewhere.' };
  }
  if (run.state === 'failed' || run.state === 'stopped') {
    if (!provider && !run.blocker && run.state === 'stopped') return none;
    const stopped = reachable;
    return { needsReview: true, stopped, headline: provider ? `Stopped: the provider is ${provider}.` : run.state === 'failed' ? 'The run failed.' : 'The run was stopped.', hold: stopped ? '' : 'The runner cannot be reached, so it is not confirmed that the run stopped.' };
  }
  return none;
}

/** Other agents that could take the work, usable now, the current one last. */
export function otherProviders(agents: readonly Agent[], current: string): Agent[] {
  return agents.filter(a => a.available && a.id !== current);
}

/** Approve is allowed only with a confirmed stop, a captured handoff and a chosen provider. */
export function approveHold(c: Continuity, hasHandoff: boolean, provider: string): string {
  if (!provider) return 'No other agent is available to take this.';
  if (!c.stopped) return c.hold || 'The previous run is not confirmed stopped.';
  if (!hasHandoff) return 'Capture the handoff first, so the next agent starts from what was recorded.';
  return '';
}
