import { describe, expect, it } from 'vitest';
import type { GitBranch, Run, Task } from './types';
import { resolveGitReviewContext } from './gitreviewcontext';

const task: Task = { id: 'task-1', projectId: 'project-1', title: 'Review sync change', description: '', state: 'review', position: 1, execution: {}, orchestration: { enabled: false, timezone: 'UTC', dependencies: [], expectedPaths: [], missedPolicy: 'run_late', missed: false }, workMode: 'agent', labelIds: [], plan: {}, version: 1, createdAt: '', updatedAt: '' };
const run: Run = { id: 'run-1', projectId: 'project-1', taskId: task.id, agentId: 'fixture', state: 'completed', policy: { interaction: 'interactive' }, version: 1, createdAt: '', updatedAt: '', endedAt: 'today', branch: 'werkbord/review-sync', headCommit: 'abc123', handoff: { objective: '', summary: 'Committed the sync change', results: 'Committed 1 change', filesChanged: [], decisions: [], tests: ['npm test (reported)'], gitState: '', knownIssues: [], blockers: [], questions: [], nextAction: '', generatedAt: '' } };
const branch: GitBranch = { name: run.branch!, ref: `refs/heads/${run.branch}`, scope: 'local', sha: 'abc123', subject: 'sync change', commitDate: '', head: false, target: false, protected: false, upstream: { state: 'none', ahead: 0, behind: 0 }, vsTarget: { ahead: 1, behind: 0, relation: 'ahead' }, merged: false, notPushed: 1, stale: false, werkbord: { created: true, namespace: true, taskId: task.id, taskTitle: task.title, taskState: 'review', phase: 'review', runId: run.id, runState: 'completed', activeRun: false }, attention: [] };
const input = (overrides: Partial<Parameters<typeof resolveGitReviewContext>[0]> = {}) => ({ projectId: task.projectId, taskId: task.id, runId: run.id, branch, branches: [branch], tasks: [task], run, ...overrides });

describe('task review context on Git branches', () => {
  it('returns the matched task, result, and reported tests', () => {
    expect(resolveGitReviewContext(input())).toMatchObject({ task, run, result: 'Committed 1 change', tests: ['npm test (reported)'] });
  });

  it.each([
    ['direct Git navigation', { taskId: '', runId: '' }],
    ['a different task', { taskId: 'task-elsewhere' }],
    ['a missing task record', { tasks: [] }],
    ['an ambiguous task record', { tasks: [task, task] }],
    ['a stale branch', { branch: { ...branch, stale: true } }],
    ['a branch linked to another run state', { branch: { ...branch, werkbord: { ...branch.werkbord, runState: 'failed' } } }],
    ['a branch moved to another commit', { branch: { ...branch, sha: 'new-sha' } }],
    ['an ambiguous branch owner', { branches: [branch, { ...branch, name: 'werkbord/duplicate' }] }],
    ['a run owned by another task', { run: { ...run, taskId: 'task-elsewhere' } }],
    ['an unfinished run', { run: { ...run, endedAt: undefined } }],
    ['a task outside Review', { tasks: [{ ...task, state: 'done' }] }],
  ])('omits context for %s', (_label, overrides) => {
    expect(resolveGitReviewContext(input(overrides as Partial<Parameters<typeof resolveGitReviewContext>[0]>))).toBeUndefined();
  });
});
