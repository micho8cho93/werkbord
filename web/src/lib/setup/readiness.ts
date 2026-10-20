import type { Agent, Project, Runner } from '../types';

export interface SetupStep { id: string; done: boolean; optional?: boolean }

/** Only an explicit optional decision can replace verification; future steps stay locked. */
export function setupResolved(step: SetupStep, states: Record<string, string>): boolean {
  return step.done || (step.optional === true && states[step.id] === 'skipped');
}

export function setupCurrent(steps: readonly SetupStep[], states: Record<string, string>, chosen: string | null): string {
  const frontier = steps.findIndex(s => !setupResolved(s, states));
  const limit = frontier < 0 ? steps.length - 1 : frontier;
  const index = steps.findIndex(s => s.id === chosen);
  if (index >= 0 && index <= limit && !(['computer', 'agents', 'repository'].includes(steps[index].id) && steps[index].done)) return steps[index].id;
  return steps[limit].id;
}

/** Recovery routes remain available while the setup gate protects ordinary navigation. */
export function setupRouteAllowed(view: string, taskId: string, exampleTaskId?: string): boolean {
  return ['onboarding', 'settings', 'help'].includes(view) || (view === 'task' && !!exampleTaskId && taskId === exampleTaskId);
}

/** Local coding readiness uses verified capabilities, never the mere existence of a project or runner. */
export function localReadiness(runners: readonly Runner[], agents: readonly Agent[], projects: readonly Project[], live: boolean) {
  const runner = runners.find(r => r.kind === 'local');
  const project = projects.find(p => p.kind !== 'work' && !!p.repoPath.trim());
  const computer = live && !!runner?.online && !runner.disabled && runner.capacity > 0;
  const agent = live && agents.some(a => a.available);
  const steps = [
    { id: 'computer', label: 'Local runner', done: computer },
    { id: 'agents', label: 'Usable coding agent', done: agent },
    { id: 'repository', label: 'Git repository', done: !!project },
  ];
  return { runner, project, steps, completed: steps.filter(s => s.done).length, ready: steps.every(s => s.done), next: steps.find(s => !s.done)?.id };
}
