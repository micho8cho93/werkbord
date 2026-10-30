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
export interface WorkPlan { id: string; projectId: string; title: string; description: string; taskIds: string[]; decisions: string; questions: string; version: number; createdAt?: string; updatedAt?: string }
