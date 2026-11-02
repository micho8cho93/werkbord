import '../../../internal/uipresentation/static/work-queue.js';
import { dependencySatisfied } from './scheduling';
import type { Task, Run } from './types';
export const WORK_GROUPS = [{ id: 'now', title: 'Now', detail: 'Active work and immediate priorities' }, { id: 'next', title: 'Next', detail: 'Ready to pick up' }, { id: 'later', title: 'Later', detail: 'Planned or waiting on dependencies' }] as const;
export function blockingTasks(task: Task, tasks: Task[], latest: Record<string, Run> = {}): {id: string; title: string}[] {
 return (task.orchestration?.dependencies ?? []).filter(id => !dependencySatisfied(tasks.find(t => t.id === id), latest[id])).map(id => ({id, title: tasks.find(t => t.id === id)?.title ?? 'Unavailable task'}));
}
export function workGroup(task: Task, tasks: Task[], run?: Run, priority = task.execution?.priority, now = Date.now(), latest: Record<string, Run> = {}) {
 return WerkbordWork.group({ done: task.state === 'done', active: task.state === 'doing' || ['starting','running','waiting_for_user'].includes(run?.state ?? ''), attention: task.state === 'review' || ['blocked','failed'].includes(run?.state ?? ''), blocked: blockingTasks(task,tasks,latest).length > 0, priority, scheduledAt: task.orchestration?.scheduledAt || task.orchestration?.notBefore, start: task.plan?.start }, now);
}
export interface PlanRun { state: 'running' | 'held'; mode: PlanMode; taskIds: string[]; startedAt: string; updatedAt: string }
export interface WorkPlan { id: string; projectId: string; title: string; description: string; taskIds: string[]; decisions: string; questions: string; run?: PlanRun; version: number; createdAt?: string; updatedAt?: string }
export type PlanMode = 'in_order' | 'as_ready';
/** The one action a plan offers now: Run plan, Pause after current task, Resume, or none. */
export type PlanControl = 'run' | 'hold' | 'resume' | 'none';
export interface PlanRunStep { taskId: string; number: number; title: string; action: 'start' | 'retry' | 'armed' | 'skip'; why?: string; after: string[]; agent?: string; runner?: string; decision?: string; reason?: string }
export interface PlanRunPreview { planId: string; planVersion: number; mode: PlanMode; state: 'idle' | 'running' | 'held'; control: PlanControl; digest: string; steps: PlanRunStep[]; toStart: number; concurrency: number; billing: string; interruption: string; review: string }

export const planControlLabel: Record<PlanControl, string> = { run: 'Run plan', hold: 'Pause after current task', resume: 'Resume', none: 'Nothing to start' };
/** What a step will do, in the words the preview shows. */
export const planActionLabel = (a: PlanRunStep['action']) => a === 'start' ? 'Will start' : a === 'retry' ? 'Will start again' : a === 'armed' ? 'Waiting to start' : 'Left alone';
