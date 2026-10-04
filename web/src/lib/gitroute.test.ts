import { describe, expect, it } from 'vitest';
import { branchHref, changeHref, changesHref, commitsHref, fileHref, overviewHref, parentOf, parseGit } from './gitroute';
import { hrefOf, parse } from './location';

const afterGit = (href: string) => parse(href).sub;

describe('git routes', () => {
  it('the overview is the default, and anything not understood is the overview', () => {
    expect(parseGit(undefined)).toEqual({ screen: 'overview' });
    expect(parseGit('')).toEqual({ screen: 'overview' });
    expect(parseGit('nonsense/1/2')).toEqual({ screen: 'overview' });
    expect(parseGit('branch/elsewhere/x')).toEqual({ screen: 'overview' });
    expect(parseGit('branch/local')).toEqual({ screen: 'overview' });
  });

  it('round-trip a branch whose name has slashes, spaces and symbols', () => {
    for (const name of ['main', 'devboard/fix-login-3k9x2m', 'feature/a b/c', 'ünï-ブランチ', 'weird?name&x=1#frag', '-dash', 'a%2Fb']) {
      for (const scope of ['local', 'remote'] as const) {
        const href = branchHref('prj_1', scope, name);
        expect(parseGit(afterGit(href))).toEqual({ screen: 'branch', scope, name });
        expect(parse(href)).toMatchObject({ view: 'git', projectId: 'prj_1' });
        expect(hrefOf({ view: 'git', projectId: 'prj_1', taskId: '', sub: afterGit(href) })).toBe(href);
      }
    }
  });

  it('round-trip a file, including a rename and a path that needs encoding', () => {
    const href = fileHref('prj_1', 'local', 'devboard/x-1', 'src/my file&more.ts', 'old/name.ts');
    expect(parseGit(afterGit(href))).toEqual({ screen: 'file', scope: 'local', name: 'devboard/x-1', path: 'src/my file&more.ts', oldPath: 'old/name.ts' });
    const plain = fileHref('prj_1', 'local', 'x', 'a.ts');
    expect(parseGit(afterGit(plain))).toMatchObject({ screen: 'file', path: 'a.ts', oldPath: '' });
  });

  it('a file link without a path is the branch', () => {
    expect(parseGit('branch/local/x/file')).toEqual({ screen: 'branch', scope: 'local', name: 'x' });
  });

  it('round-trip working changes, and a file of them', () => {
    expect(parseGit(afterGit(changesHref('prj_1')))).toEqual({ screen: 'changes', worktree: '' });
    expect(parseGit(afterGit(changesHref('prj_1', 'wt_9')))).toEqual({ screen: 'changes', worktree: 'wt_9' });
    expect(parseGit(afterGit(changeHref('prj_1', '', 'staged', 'a b.ts')))).toEqual({ screen: 'change', worktree: '', kind: 'staged', path: 'a b.ts', oldPath: '' });
    expect(parseGit(afterGit(changeHref('prj_1', 'wt_9', 'untracked', 'x/y.ts', 'z.ts')))).toEqual({ screen: 'change', worktree: 'wt_9', kind: 'untracked', path: 'x/y.ts', oldPath: 'z.ts' });
    // A kind that is not one is not guessed at.
    expect(parseGit('change/main?kind=evil&path=a')).toEqual({ screen: 'changes', worktree: '' });
    expect(parseGit('change/main?kind=staged')).toEqual({ screen: 'changes', worktree: '' });
  });

  it('round-trip a history', () => {
    expect(parseGit(afterGit(commitsHref('prj_1', 'remote', 'origin/feature/x')))).toEqual({ screen: 'commits', scope: 'remote', name: 'origin/feature/x' });
  });

  it('survive mangled encodings', () => {
    expect(parseGit('branch/local/%E0%A4%A')).toEqual({ screen: 'overview' });
    expect(parseGit('changes/%E0%A4%A')).toEqual({ screen: 'changes', worktree: '' });
  });

  it('the overview has the bare address, so older links keep working', () => {
    expect(overviewHref('prj_1')).toBe('#/p/prj_1/git');
    expect(parse('#/p/prj_1/git')).toEqual({ view: 'git', projectId: 'prj_1', taskId: '' });
    expect(parse('#/git')).toEqual({ view: 'git', projectId: '', taskId: '' });
  });

  it('back goes up one level of the drill-down', () => {
    expect(parentOf('p', { screen: 'file', scope: 'local', name: 'x', path: 'a', oldPath: '' })).toBe(branchHref('p', 'local', 'x'));
    expect(parentOf('p', { screen: 'branch', scope: 'local', name: 'x' })).toBe(overviewHref('p'));
    expect(parentOf('p', { screen: 'change', worktree: 'wt_1', kind: 'staged', path: 'a', oldPath: '' })).toBe(changesHref('p', 'wt_1'));
    expect(parentOf('p', { screen: 'changes', worktree: '' })).toBe(overviewHref('p'));
  });
});
