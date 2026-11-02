export interface FallbackSettings { enabled: boolean; providers: string[]; maxSwitches: number; dailyLimit: number }
export interface FallbackEntry { runId: string; taskId: string; agent: string; result: 'claimed' | 'started' | 'declined' | 'retry'; reason?: string; newRunId?: string; at: string }
export interface FallbackView extends FallbackSettings { ledger: FallbackEntry[] }

/** What a ledger entry says, in words that do not claim more than was recorded. */
export function fallbackLine(e: FallbackEntry, agentName: (id: string) => string): string {
  switch (e.result) {
    case 'started': return `Continued with ${agentName(e.agent)}.`;
    case 'declined': return `Not continued with ${agentName(e.agent)}: ${(e.reason ?? 'no reason recorded').replace(/^not continued automatically: /, '')}.`;
    case 'retry': return `Waiting to try ${agentName(e.agent)} again: ${e.reason ?? 'a limit was reached'}.`;
    default: return `Reserved for ${agentName(e.agent)}. Nothing is known to have started, and it will not be tried again on its own.`;
  }
}

/** The providers a person can pick: usable agents, in the order they were chosen first, then the rest. */
export function orderedChoices(all: readonly string[], chosen: readonly string[]): string[] {
  return [...chosen.filter(id => all.includes(id)), ...all.filter(id => !chosen.includes(id))];
}

export function fallbackProblem(s: FallbackSettings): string {
  if (s.enabled && s.providers.length === 0) return 'Choose at least one agent that may take over.';
  if (!Number.isInteger(s.maxSwitches) || s.maxSwitches < 1 || s.maxSwitches > 3) return 'Automatic switches per task: 1 to 3.';
  if (!Number.isInteger(s.dailyLimit) || s.dailyLimit < 1 || s.dailyLimit > 20) return 'Automatic switches per day: 1 to 20.';
  return '';
}
