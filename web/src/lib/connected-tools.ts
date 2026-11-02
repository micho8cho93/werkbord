// What the "Agents & connections" popover says, from the app's reduced reading (shell/types.ts AgentsAndConnections): the coding agents
// it found, the connected tools and the period's usage. Pure. It states only what was detected or reported: no remaining allowance, no
// reset date and no cost is invented, and nothing here is ever put in a Team report.

import { measuredCost } from './analytics';
import { serviceFor } from './mcp-catalog';

export interface AgentReading { id: string; name: string; signedIn: boolean; active: number; limitEvents: number }
export interface ToolReading { id: string; name: string; service?: string; state: 'verified' | 'untested' | 'failed' }

export interface CodingAgentRow {
  id: string;
  name: string;
  /** "Signed in" or "Installed": only what the app detected. */
  status: 'Signed in' | 'Installed';
  /** The person's own live sessions of this agent. */
  active: number;
  /** "Limit unknown" unless the period's usage reports that the provider's limit was reached. */
  limit: string;
}

export function codingAgents(agents: readonly AgentReading[]): CodingAgentRow[] {
  return agents.map(a => ({
    id: a.id,
    name: a.name,
    status: a.signedIn ? 'Signed in' : 'Installed',
    active: a.active,
    limit: a.limitEvents > 0 ? `Limit reached ${a.limitEvents} ${a.limitEvents === 1 ? 'time' : 'times'} this period` : 'Limit unknown',
  }));
}

export interface ToolRow { id: string; name: string; state: string; /** The service's mark, when the address is a known service. */ service?: { id: string; name: string } }

const TOOL_STATE: Record<ToolReading['state'], string> = { verified: 'Verified', untested: 'Not yet tested', failed: 'Last test failed' };

/** Connected tools, "Verified" only when the app has tested the connection. */
export function connectedTools(tools: readonly ToolReading[]): ToolRow[] {
  return tools.map(t => ({ id: t.id, name: t.name, state: TOOL_STATE[t.state] ?? TOOL_STATE.untested, service: serviceFor(t.service) }));
}

export interface UsageLines {
  /** What providers reported as API charges. Never added to the estimate. */
  reported: string;
  /** What the same use would cost at API prices. An estimate, not a charge. */
  estimated: string;
  caveat: string;
}

export const USAGE_CAVEAT = 'Balances and reset times are not available.';

/** The period's usage as two separate figures. Only named fields are read: nothing about an allowance can come through. */
export function usageLines(usage: { reported: number | null; estimated: number | null } | null | undefined): UsageLines | undefined {
  if (!usage) return undefined;
  return { reported: measuredCost(usage.reported ?? null), estimated: measuredCost(usage.estimated ?? null), caveat: USAGE_CAVEAT };
}
