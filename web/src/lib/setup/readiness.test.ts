import { expect, it } from 'vitest';
import { localReadiness } from './readiness';
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
