import { describe, expect, it, vi } from 'vitest';
import { changesBetween, createRepoWatcher } from './repowatch';

describe('changesBetween', () => {
  it('treats the first look as the baseline', () => {
    expect(changesBetween('o/r', undefined, [{ name: 'main', sha: 'a' }])).toEqual([]);
  });
  it('reports a moved branch and a new one, not an unchanged one', () => {
    const now = [{ name: 'main', sha: 'b' }, { name: 'dev', sha: 'c' }, { name: 'new', sha: 'd' }];
    expect(changesBetween('o/r', { main: 'a', dev: 'c' }, now)).toEqual([
      { repo: 'o/r', branch: 'main', sha: 'b', previous: 'a' },
      { repo: 'o/r', branch: 'new', sha: 'd', created: true },
    ]);
  });
});

describe('createRepoWatcher', () => {
  it('reports a push on the second look only, and ignores an empty answer', async () => {
    const answers = [[{ name: 'main', sha: 'a' }], [], [{ name: 'main', sha: 'b' }]];
    const report = vi.fn();
    const w = createRepoWatcher({ heads: async () => answers.shift() ?? [], report, every: 1e9 });
    w.set(['o/r', 'bad name']);
    await w.tick();
    await w.tick();
    expect(report).not.toHaveBeenCalled();
    await w.tick();
    expect(report).toHaveBeenCalledWith({ repo: 'o/r', branch: 'main', sha: 'b', previous: 'a' });
    w.stop();
  });
});
