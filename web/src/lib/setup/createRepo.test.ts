import { describe, expect, it } from 'vitest';
import { createPlan, repoNameProblem } from './createRepo';

describe('repoNameProblem', () => {
  it('accepts ordinary names', () => {
    expect(repoNameProblem('werkbord-hello')).toBe('');
    expect(repoNameProblem('a.b_c-1')).toBe('');
  });
  it('rejects names GitHub would refuse', () => {
    for (const bad of ['', '  ', 'has space', 'a/b', '..', 'x.git', 'é']) expect(repoNameProblem(bad), bad).not.toBe('');
  });
});

describe('createPlan', () => {
  it('names the account, the repository and the visibility before anything is created', () => {
    const plan = createPlan('octo', ' hello ', false);
    expect(plan[0]).toBe('Create a public repository octo/hello on GitHub');
    expect(plan).toHaveLength(3);
  });
  it('warns that Pages may not be available for a private repository', () => {
    expect(createPlan('octo', 'hello', true)[1]).toContain('paid GitHub plan');
  });
});
