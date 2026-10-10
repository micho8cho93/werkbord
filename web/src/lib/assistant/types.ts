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
}

export interface SessionView {
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

export interface ToolEvent {
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
  workspace?: { id: string; workspaceId: string; method: string; args: unknown };
  seq: number;
  sessionId: string;
  turnId?: string;
  type: EventType;
  at: string;
  attempt?: number;
  text?: string;
  tool?: ToolEvent;
  proposal?: { actionId: string; operation: string; summary: string; expiresAt: string };
  actionId?: string;
  outcome?: string;
  error?: ErrorInfo;
  usage?: { inputTokens?: number; outputTokens?: number };
  state?: SessionView['state'];
}
