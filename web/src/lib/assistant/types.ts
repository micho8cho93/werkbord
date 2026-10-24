// What the assistant's API speaks (internal/api/assistant.go, internal/assistant/events.go). Nothing here is the board's own
// vocabulary: the assistant is reached only through these.

export interface ProviderModel {
  id: string;
  name: string;
  description?: string;
  default?: boolean;
  /** The reasoning levels this model takes; empty where the provider does not say. */
  reasoning?: string[];
}

export interface ProviderModels {
  providerId: string;
  models: ProviderModel[];
  /** Levels that apply to every model, for a provider that does not tie them to one. */
  reasoning?: string[];
  custom: boolean;
  note?: string;
}

export interface ProviderInfo {
  id: string;
  name: string;
  installed: boolean;
  signedIn?: boolean | null;
  available: boolean;
  version?: string;
  usesApiKey?: boolean;
  auth?: string;
  detail?: string;
  guidance?: string;
  capabilities: { streaming: string; resume: boolean; cancel: boolean; modelChoice: boolean; reasoningChoice: boolean; voice: string; notes?: string[] };
  models: ProviderModels;
}

export interface AttachmentPolicy {
  enabled: boolean;
  maxBytes: number;
  maxFiles: number;
  maxBytesTotal: number;
  retentionSeconds: number;
}

export interface ProvidersResponse {
  providers: ProviderInfo[];
  attachments: AttachmentPolicy;
}

export interface AttachmentInfo {
  id: string;
  name: string;
  bytes: number;
  expiresAt: string;
}

/** A change the assistant proposed, waiting for the person. */
export interface PendingAction {
  id: string;
  operation: string;
  summary: string;
  expiresAt: string;
  args?: Record<string, unknown>;
}

export interface SessionView {
 mode?: import('../selection').SelectionMode;
 selection?: import('../selection').SelectionDecision;
  id: string;
  provider: string;
  model?: string;
  reasoning?: string;
  state: 'idle' | 'running' | 'awaiting_confirmation';
  turns: number;
  lastError?: string;
  running: boolean;
  /** The server's newest event number. Lower than what a client has seen means the controller restarted. */
  lastEvent: number;
  pending: PendingAction[];
  attachments: AttachmentInfo[];
}

export interface MemorySource {
 id: string; summary: string; kind: string; freshness: string; verification: string; updatedAt: string;
 /** Whose memory it is: private to this computer, or shared through a Team workspace. */
 origin?: 'individual' | 'team';
 source: { reference: string };
}

export interface ToolEvent {
 sources?: MemorySource[];
  /** The rules' own answer to recommend_next_actions, exactly as the controller returned it (never a model's wording). */
  recommendations?: import('../recommend/types').RecommendationSet;
  callId: string;
  name: string;
  status?: 'ok' | 'error' | 'pending_confirmation';
  code?: string;
  detail?: string;
}

export interface ErrorInfo {
  code: string;
  message: string;
  retryable: boolean;
}

export type EventType =
  | 'workspace_request'
  | 'turn_started'
  | 'delta'
  | 'tool_call'
  | 'tool_result'
  | 'confirmation_required'
  | 'confirmation_resolved'
  | 'notice'
  | 'retry'
  | 'turn_completed'
  | 'turn_failed'
  | 'turn_cancelled'
  | 'gap';

export interface AssistantEvent {
 selection?: import('../selection').SelectionDecision;
  workspace?: { id: string; proposalId?: string; workspaceId: string; method: string; args: unknown };
  seq: number;
  sessionId: string;
  turnId?: string;
  type: EventType;
  at: string;
  providerAt?: string;
  /** Monotonic browser receipt time, added locally before the frame-coalesced update. */
  clientReceivedAt?: number;
  attempt?: number;
  text?: string;
  tool?: ToolEvent;
  proposal?: { actionId: string; operation: string; summary: string; args?: Record<string, unknown>; expiresAt: string };
  actionId?: string;
  outcome?: string;
  ticket?: { id: string; projectId: string; title: string; state: string };
  error?: ErrorInfo;
  usage?: { inputTokens?: number; outputTokens?: number };
  state?: SessionView['state'];
}
