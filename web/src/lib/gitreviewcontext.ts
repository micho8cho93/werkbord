import type { GitBranch, Run, Task } from './types';

export interface GitReviewContext {
  task: Task;
  run: Run;
  result: string;
  tests: string[];
}

/** Accept context only for the explicit origin pair when current task, run and branch all agree. */
export function resolveGitReviewContext(input: {
  projectId: string;
  taskId: string;
  runId: string;
  branch: GitBranch;
  branches: GitBranch[];
  tasks: Task[];
  run?: Run;
}): GitReviewContext | undefined {
  const { projectId, taskId, runId, branch, branches, tasks, run } = input;
  if (!taskId || !runId || branch.stale || branch.scope !== 'local') return;
  const matches = branches.filter((b) => b.scope === 'local' && b.devboard.taskId === taskId && b.devboard.runId === runId);
  if (matches.length !== 1 || matches[0].name !== branch.name || matches[0].sha !== branch.sha) return;
  if (!run || run.id !== runId || run.projectId !== projectId || run.taskId !== taskId || !run.endedAt || run.remote) return;
  if (branch.devboard.taskId !== taskId || branch.devboard.runId !== runId || branch.devboard.runState !== run.state || branch.devboard.taskState !== 'review' || branch.devboard.activeRun) return;
  const taskMatches = tasks.filter((t) => t.id === taskId && t.projectId === projectId && t.state === 'review' && !t.archivedAt);
  const task = taskMatches[0];
  if (taskMatches.length !== 1 || task.title !== branch.devboard.taskTitle) return;
  if (run.branch && run.branch !== branch.name) return;
  if (run.headCommit && run.headCommit !== branch.sha) return;
  const result = run.handoff?.results || run.handoff?.summary || '';
  if (!result) return;
  return { task, run, result, tests: run.handoff?.tests ?? [] };
}
