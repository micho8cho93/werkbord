export type AgentStateInput = 'requested' | 'queued' | 'starting' | 'running' | 'needs_input' | 'blocked' | 'completed' | 'failed' | 'stopped';
export type AgentRing = 'queued' | 'starting' | 'running' | 'waiting' | 'blocked' | 'completed' | 'failed' | 'stopped' | 'stale' | 'unknown';

export interface AgentStateRequest {
  state?: string;
  fresh?: boolean | null;
  observedAt?: string | number;
  now?: number;
  reducedMotion?: boolean;
  agentName?: string;
}

export interface AgentStateView {
  kind: AgentStateInput | 'stale' | 'unknown';
  ring: AgentRing;
  animate: boolean;
  arc: boolean;
  label: string;
  text: string;
}

declare global {
  var WerkbordIdentity: {
    STATES: AgentStateInput[];
    TONES: number;
    resolveAgentState(input?: AgentStateRequest): AgentStateView;
    ageLabel(observedAt: string | number | undefined, now: number): string;
    initials(name: string | undefined): string;
    avatarTone(id: string | undefined): number;
  };
}
