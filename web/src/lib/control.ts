import type { ExecutionConfig, Task } from './types';

/** An undetermined automatic choice remains in each eligible machine's queue view. */
export function matchesScheduledExecution(task: Task, project: ExecutionConfig, global: ExecutionConfig, runner: string, agent: string): boolean {
  const assigned = task.execution.runner || project.runner || global.runner || 'automatic';
  const required = task.execution.agent || project.agent || global.agent || '';
  return (!runner || assigned === 'automatic' || assigned === runner) && (!agent || !required || required === agent);
}
