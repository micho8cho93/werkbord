import { request } from '../api';
import type { Run, Task } from '../types';
import type { DeckKeyId } from './deck';
import type { IconName } from '../Icon.svelte';

export const WORKFLOWS = [
  { id: 'find-bugs', label: 'Find Bugs', icon: 'search', description: 'Report reproducible bugs and suggested fixes. Leaves files unchanged.' },
  { id: 'improve-tests', label: 'Improve Tests', icon: 'check', description: 'Add focused regression tests and run relevant checks in a task worktree.' },
  { id: 'security-audit', label: 'Security Audit', icon: 'control', description: 'Report security risks and remediation. Leaves files unchanged.' },
  { id: 'dependency-check', label: 'Dependency Check', icon: 'branch', description: 'Report dependency risks and recommended updates. Does not update packages.' },
  { id: 'refactor-codebase', label: 'Refactor Codebase', icon: 'git', description: 'Make one small, behaviour-preserving refactor in a task worktree.' },
  { id: 'sprint-summary', label: 'Sprint Summary', icon: 'runs', description: 'Summarise Git activity from the last 14 days. Leaves files unchanged.' },
] as const satisfies readonly { id: string; label: string; icon: IconName; description: string }[];
export type WorkflowId = typeof WORKFLOWS[number]['id'];
export type ButtonId = DeckKeyId | WorkflowId;
export interface DeckSlot { id: string; button: ButtonId; projectId?: string; agent?: string; model?: string }
export interface DeckLayout { version: number; slots: (DeckSlot | null)[] }
export interface DeckExecution { requestId: string; slotId: string; projectId: string; taskId: string }
export function workflow(id: string) { return WORKFLOWS.find(w => w.id === id); }
export function initialLayout(): DeckLayout {
  return { version: 0, slots: [...(['needs', 'calendar', 'projects', 'mywork', 'agents', 'analytics'] as DeckKeyId[]).map(button => ({ id: button, button })), null, null, null] };
}
export function place(slots: (DeckSlot | null)[], index: number, slot: DeckSlot | null): (DeckSlot | null)[] {
  if (slots.length !== 9 || !Number.isInteger(index) || index < 0 || index > 8) throw new Error('A deck has nine positions.');
  return slots.map((s, i) => i === index ? slot : s);
}
/** Swap positions; dropping on an empty socket moves the empty socket back. */
export function rearrange(slots: (DeckSlot | null)[], from: number, to: number): (DeckSlot | null)[] {
  if (slots.length !== 9 || !Number.isInteger(from) || !Number.isInteger(to) || Math.min(from, to) < 0 || Math.max(from, to) > 8) throw new Error('Choose a position from 1 to 9.');
  const out = [...slots]; [out[from], out[to]] = [out[to], out[from]]; return out;
}
export const deckApi = {
  load: () => request<DeckLayout>('GET', '/api/deck'),
  save: (layout: DeckLayout) => request<DeckLayout>('PUT', '/api/deck', layout),
  history: () => request<DeckExecution[]>('GET', '/api/deck/history'),
  start: (slotId: string, requestId: string) => request<{ task: Task; run: Run }>('POST', '/api/deck/workflows', { slotId, requestId }),
};
