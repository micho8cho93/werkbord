export type Memory = {
  id: string;
  scope: { ownerId: string; projectId: string; repository?: string };
  type: string;
  authority: string;
  summary: string;
  state: string;
  pinned: boolean;
  verifiedAt?: string;
  version: number;
  source: MemorySource;
};
export type MemorySource = { type: string; reference: string; runId?: string; handoffId?: string; gitSha?: string; branch?: string; approvedBy?: string; approvalRef?: string; paths?: string[] };
export type MemoryEvidence = { key: string; source: MemorySource; excerpt: string; observedAt: string };
export type ManualMemory = { summary: string; kind: string; corrects?: string; version?: number; repository?: string };
export function memoryLabel(memory: Memory): string {
  if (memory.authority === 'approved_policy') return 'Saved policy';
  if (memory.authority === 'explicit_decision') return 'Decision';
  if (memory.authority === 'inferred_preference') return 'Preference';
  return memory.verifiedAt && memory.state === 'current' ? 'Verified fact' : 'Suggestion';
}
export function verificationLabel(memory: Memory): string {
  if (memory.state === 'historical' || memory.state === 'superseded') return 'Historical';
  if (!memory.verifiedAt || memory.state !== 'current') return 'Unverified';
  return memory.source.type === 'user' ? 'Confirmed by you' : 'Verified by controller';
}
