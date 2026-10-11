import type { Agent, Project, Runner } from '../types';

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
