import { request } from '../api';
import type { Run, Task } from '../types';
import type { DeckKeyId } from './deck';
import type { IconName } from '../Icon.svelte';

export const WORKFLOWS = [
  { id: 'find-bugs', label: 'Find Bugs', icon: 'search', description: 'Report reproducible bugs and suggested fixes. Leaves files unchanged.' },
  { id: 'improve-tests', label: 'Improve Tests', icon: 'check', description: 'Find coverage gaps, propose tests and validate approved changes.' },
  { id: 'security-audit', label: 'Security Audit', icon: 'control', description: 'Report security risks and remediation. Leaves files unchanged.' },
  { id: 'dependency-check', label: 'Dependency Check', icon: 'branch', description: 'Report dependency risks and recommended updates. Does not update packages.' },
  { id: 'refactor-codebase', label: 'Refactor Codebase', icon: 'git', description: 'Propose targeted refactors and validate approved changes.' },
  { id: 'sprint-summary', label: 'Sprint Summary', icon: 'runs', description: 'Summarise tasks, blockers, agent activity and Git activity from the last 14 days.' },
] as const satisfies readonly { id: string; label: string; icon: IconName; description: string }[];
export type WorkflowId = typeof WORKFLOWS[number]['id'];
/** A generated personal workspace's button: the workspace's own ID. It opens that workspace's page, like a navigation key. */
export type WorkspaceButtonId = `pws_${string}`;
export const isWorkspaceButton = (id: string): id is WorkspaceButtonId => /^pws_[a-z2-7]{16}$/.test(id);
export type ButtonId = DeckKeyId | WorkflowId | WorkspaceButtonId;
export interface DeckSlot { id: string; button: ButtonId; projectId?: string; agent?: string; model?: string; reasoning?: string; runner?: string; target?: 'current' | 'selected' | 'all' | 'ask'; projectIds?: string[]; output?: 'report' | 'drafts' | 'changes'; approval?: 'always' | 'changes'; maxRepositories?: number }
export interface DeckStart { approved?: boolean; currentProjectId?: string; projectIds?: string[]; override?: DeckSlot }
export function targetLabel(slot: DeckSlot, projects: readonly {id: string; name: string}[], current: string): string {
 const names = (ids: string[]) => ids.map(id => projects.find(p => p.id === id)?.name ?? 'Unavailable').join(', ');
 if (slot.target === 'all') return 'All accessible repositories';
 if (slot.target === 'ask') return 'Ask every time';
 if (slot.target === 'selected') { const ids = slot.projectIds ?? []; return ids.length > 1 ? `${ids.length} repos: ${names(ids)}` : names(ids) || 'Choose repositories'; }
 if (!slot.target && slot.projectId) return names([slot.projectId]);
 return `Current: ${names([current]) || 'Choose project'}`;
}
export interface DeckLayout { version: number; slots: (DeckSlot | null)[] }
export interface DeckExecution { requestId: string; slotId: string; projectId: string; taskId: string; configuration?: DeckSlot; findings?: Task[] }
export function workflow(id: string) { return WORKFLOWS.find(w => w.id === id); }
/** A new workflow asks for its scope on tap; holding it edits the saved defaults. */
export function newSlot(id: string, button: ButtonId): DeckSlot {
  return { id, button, ...(workflow(button) ? { target: 'ask' as const, output: ['find-bugs', 'security-audit', 'dependency-check'].includes(button) ? 'drafts' as const : 'report' as const } : {}) };
}
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
  start: (slotId: string, requestId: string, input: DeckStart = {}) => request<{ task: Task; run: Run; tasks: Task[]; runs: Run[] }>('POST', '/api/deck/workflows', { slotId, requestId, ...input }),
};
