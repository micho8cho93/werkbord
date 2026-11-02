import { beforeEach, describe, expect, it, vi } from 'vitest';
import { gitHead } from './git-head.svelte';

const ProjectHead = vi.fn();
beforeEach(() => {
  ProjectHead.mockReset();
  vi.stubGlobal('window', { go: { main: { App: { ProjectHead } } } });
});

describe('the header\'s Git read', () => {
  it('reads a project once, and again only after a while', async () => {
    ProjectHead.mockResolvedValue({ branch: 'main', commit: 'a1b2c3d' });
    await gitHead.load('prj_1', 1_000_000);
    await gitHead.load('prj_1', 1_000_500);
    expect(ProjectHead).toHaveBeenCalledTimes(1);
    expect(gitHead.head).toEqual({ branch: 'main', commit: 'a1b2c3d' });
    await gitHead.load('prj_1', 1_000_000 + 20_000);
    expect(ProjectHead).toHaveBeenCalledTimes(2);
  });

  it('forgets the last project at once when another is chosen, and says nothing when the read fails', async () => {
    ProjectHead.mockResolvedValueOnce({ branch: 'main', commit: 'a1b2c3d' }).mockRejectedValueOnce(new Error('no repository'));
    await gitHead.load('prj_a', 5_000_000);
    const pending = gitHead.load('prj_b', 5_000_100);
    expect(gitHead.head).toBeNull();
    await pending;
    expect(gitHead.head).toBeNull();
  });

  it('is cleared when there is no Personal project in view, and never reads then', async () => {
    ProjectHead.mockResolvedValue({ branch: 'main', commit: 'a1b2c3d' });
    await gitHead.load('prj_c', 9_000_000);
    ProjectHead.mockClear();
    await gitHead.load(null);
    expect(gitHead.head).toBeNull();
    expect(ProjectHead).not.toHaveBeenCalled();
  });
});
