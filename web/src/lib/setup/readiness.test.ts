import { expect, it } from 'vitest';
import { localReadiness, setupCurrent, setupResolved, setupRouteAllowed } from './readiness';
import type { Agent, Project, Runner } from '../types';

const runner = { kind: 'local', online: true, disabled: false, capacity: 1 } as Runner;
const agent = { available: true } as Agent;
const repo = { id: 'repo', repoPath: '/code/app', kind: 'repository' } as Project;

it('requires a usable local runner, agent and Git repository without optional connections', () => {
  expect(localReadiness([runner], [agent], [repo], true)).toMatchObject({ ready: true, completed: 3, project: repo });
  expect(localReadiness([runner], [], [repo], true)).toMatchObject({ ready: false, completed: 2, next: 'agents' });
  expect(localReadiness([{ ...runner, disabled: true }], [agent], [repo], true).ready).toBe(false);
  expect(localReadiness([{ ...runner, capacity: 0 }], [agent], [repo], true).ready).toBe(false);
  expect(localReadiness([{ ...runner, kind: 'remote' }], [agent], [repo], true).ready).toBe(false);
  expect(localReadiness([runner], [agent], [repo], false)).toMatchObject({ ready: false, next: 'computer' });
});

it('does not count a planning project as a repository and selects a registered clone', () => {
  const work = { id: 'work', kind: 'work', repoPath: '' } as Project;
  expect(localReadiness([runner], [agent], [work], true)).toMatchObject({ completed: 2, next: 'repository', project: undefined });
  expect(localReadiness([runner], [agent], [work, repo], true).project).toBe(repo);
});

it('advances a selected setup step when detection verifies it and keeps completed requirements out of the path', () => {
  const steps = [...localReadiness([runner], [agent], [repo], true).steps, { id: 'github', done: false, optional: true }, { id: 'ticket', done: false }];
  for (const id of ['computer', 'agents', 'repository']) expect(setupCurrent(steps, {}, id)).toBe('github');
  expect(setupCurrent(steps, {github:'skipped'}, 'computer')).toBe('ticket');
  expect(setupCurrent(steps, {}, 'ticket')).toBe('github');
  const disconnected = [...localReadiness([runner], [agent], [repo], false).steps, {id:'github',done:false}];
  expect(setupCurrent(disconnected, {}, 'computer')).toBe('computer');
});

it('ignores skips for required capabilities and locks future steps until optional choices are resolved', () => {
  const steps = [{id:'agents', done:false}, {id:'github', done:false, optional:true}, {id:'review',done:false}];
  expect(setupResolved(steps[0], {agents:'skipped'})).toBe(false);
  expect(setupCurrent(steps, {agents:'skipped',github:'skipped'}, 'review')).toBe('agents');
  steps[0].done = true;
  expect(setupCurrent(steps, {}, 'review')).toBe('github');
  expect(setupCurrent(steps, {github:'skipped'}, 'review')).toBe('review');
});

it('keeps recovery and the exact example task reachable, while blocking unrelated navigation', () => {
  for (const view of ['settings', 'help', 'onboarding']) expect(setupRouteAllowed(view, '')).toBe(true);
  expect(setupRouteAllowed('task', 'example', 'example')).toBe(true);
  expect(setupRouteAllowed('task', 'other', 'example')).toBe(false);
  for (const view of ['board', 'projects', 'control', 'analytics']) expect(setupRouteAllowed(view, '')).toBe(false);
});
