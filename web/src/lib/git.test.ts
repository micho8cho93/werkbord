import { describe, expect, it } from 'vitest';
import {
  branchActions,
  branchChips,
  diffLineKind,
  filterBranches,
  headline,
  mergeableLabel,
  needsAttention,
  openPRsByBranch,
  outcomeTitle,
  outcomeTone,
  quickActions,
  relationLabel,
  safeURL,
  splitBranch,
  splitPath,
  upstreamLabel,
} from './gitui';
import type { GitBranch, GitHubPR } from './types';

function branch(over: Partial<GitBranch> = {}): GitBranch {
  return {
    name: 'devboard/fix-abc123',
    ref: 'refs/heads/devboard/fix-abc123',
    scope: 'local',
    sha: 'a'.repeat(40),
    subject: 'fix',
    commitDate: '2026-01-01T00:00:00Z',
    head: false,
    target: false,
    protected: false,
    upstream: { state: 'none', ahead: 0, behind: 0 },
    vsTarget: { ahead: 2, behind: 0, relation: 'ahead' },
    merged: false,
    notPushed: 2,
    stale: false,
    devboard: { created: true, namespace: true, phase: 'review', activeRun: false, taskId: 'tsk_1', runId: 'run_1' },
    attention: [],
    ...over,
  };
}

const ctx = { github: true, hasOpenPR: false };

describe('labels', () => {
  it('say how a branch stands against the target', () => {
    expect(relationLabel(branch(), 'main')).toBe('2 ahead of main');
    expect(relationLabel(branch({ vsTarget: { ahead: 3, behind: 2, relation: 'diverged' } }), 'main')).toBe('diverged from main: 3 ahead, 2 behind');
    expect(relationLabel(branch({ vsTarget: { ahead: 0, behind: 4, relation: 'merged' } }), 'main')).toBe('merged into main');
    expect(relationLabel(branch({ vsTarget: { ahead: 0, behind: 3, relation: 'behind' } }), 'main')).toBe('behind main by 3, with no commits of its own');
    expect(relationLabel(branch({ vsTarget: { ahead: 0, behind: 0, relation: 'same' } }), 'main')).toBe('nothing beyond main');
    expect(relationLabel(branch({ vsTarget: { ahead: 0, behind: 0, relation: 'unknown' } }), '')).toBe('not compared');
  });

  it('say how a branch stands against its upstream, and are plain about it', () => {
    expect(upstreamLabel({ state: 'none', ahead: 0, behind: 0 })).toBe('no upstream');
    expect(upstreamLabel({ state: 'ahead', ahead: 1, behind: 0, name: 'origin/x' })).toBe('1 commit not pushed');
    expect(upstreamLabel({ state: 'behind', ahead: 0, behind: 2, name: 'origin/x' })).toBe('2 commits behind origin/x');
    expect(upstreamLabel({ state: 'diverged', ahead: 1, behind: 2, name: 'origin/x' })).toBe('diverged from origin/x: 1 ahead, 2 behind');
    expect(upstreamLabel({ state: 'gone', ahead: 0, behind: 0, name: 'origin/x' })).toBe('origin/x was deleted');
    expect(upstreamLabel({ state: 'in_sync', ahead: 0, behind: 0, name: 'origin/x' })).toBe('in sync with origin/x');
  });

  it('split names for display without losing them', () => {
    expect(splitBranch('devboard/fix-abc123')).toEqual({ prefix: 'devboard/', rest: 'fix-abc123' });
    expect(splitBranch('a/b/c')).toEqual({ prefix: 'a/b/', rest: 'c' });
    expect(splitBranch('main')).toEqual({ prefix: '', rest: 'main' });
    expect(splitPath('src/lib/a.ts')).toEqual({ dir: 'src/lib/', name: 'a.ts' });
    expect(splitPath('a.ts')).toEqual({ dir: '', name: 'a.ts' });
  });

  it('give a card its chips', () => {
    const texts = (b: GitBranch) => branchChips(b, 'main').map((c) => c.text);
    expect(texts(branch())).toEqual(['2 ahead', 'not on any remote']);
    expect(texts(branch({ upstream: { state: 'ahead', ahead: 1, behind: 0, name: 'origin/x' }, notPushed: 1 }))).toEqual(['2 ahead', '1 to push']);
    expect(texts(branch({ vsTarget: { ahead: 0, behind: 1, relation: 'merged' }, merged: true, notPushed: 0 }))).toEqual(['merged', 'local only']);
    expect(texts(branch({ worktree: { path: '/w', primary: false, owned: true, dirty: { staged: 1, unstaged: 2, untracked: 0, conflicted: 0 } } }))).toContain('3 uncommitted');
    const live = { ...branch().devboard, activeRun: true };
    expect(texts(branch({ devboard: { ...live, runState: 'running' } }))).toContain('agent working');
    expect(texts(branch({ devboard: { ...live, runState: 'waiting_for_user', runWaiting: 'idle' } }))).toContain('session idle');
    expect(texts(branch({ devboard: { ...live, runState: 'waiting_for_user', runWaiting: 'question' } }))).toContain('needs your answer');
    expect(texts(branch({ devboard: { ...live, runState: 'blocked' } }))).toContain('agent blocked');
    expect(texts(branch())).not.toContain('agent working');
    expect(texts(branch({ unusual: 'x' }))).toContain('unusual name');
    expect(texts(branch({ target: true, vsTarget: { ahead: 0, behind: 0, relation: 'target' }, upstream: { state: 'in_sync', ahead: 0, behind: 0 } }))).toEqual(['target']);
  });
});

describe('what needs attention', () => {
  it('is a local branch with an action or a warning, never the target', () => {
    expect(needsAttention(branch({ attention: [{ kind: 'review', severity: 'action', message: 'x' }] }))).toBe(true);
    expect(needsAttention(branch({ attention: [{ kind: 'unpushed', severity: 'warn', message: 'x' }] }))).toBe(true);
    expect(needsAttention(branch({ attention: [{ kind: 'behind', severity: 'info', message: 'x' }] }))).toBe(false);
    expect(needsAttention(branch({ scope: 'remote', attention: [{ kind: 'review', severity: 'action', message: 'x' }] }))).toBe(false);
    expect(needsAttention(branch({ target: true, attention: [{ kind: 'review', severity: 'action', message: 'x' }] }))).toBe(false);
  });

  it('puts the most pressing reason first', () => {
    const b = branch({
      attention: [
        { kind: 'stale', severity: 'warn', message: 'stale' },
        { kind: 'review', severity: 'action', message: 'review' },
        { kind: 'behind', severity: 'info', message: 'behind' },
      ],
    });
    expect(headline(b)?.kind).toBe('review');
    expect(headline(branch())).toBeUndefined();
  });

  it('filters', () => {
    const list = [
      branch({ name: 'a', attention: [{ kind: 'review', severity: 'action', message: '' }] }),
      branch({ name: 'b', devboard: { created: false, namespace: false, phase: 'none', activeRun: false }, vsTarget: { ahead: 0, behind: 1, relation: 'merged' } }),
      branch({ name: 'origin/c', scope: 'remote' }),
      branch({ name: 'main', target: true, vsTarget: { ahead: 0, behind: 0, relation: 'target' } }),
    ];
    const names = (f: Parameters<typeof filterBranches>[1]) => filterBranches(list, f).map((b) => b.name);
    expect(names('attention')).toEqual(['a']);
    expect(names('devboard')).toEqual(['a', 'origin/c', 'main']);
    expect(names('local')).toEqual(['a', 'b', 'main']);
    expect(names('remote')).toEqual(['origin/c']);
    expect(names('merged')).toEqual(['b']);
    expect(names('all')).toHaveLength(4);
  });
});

describe('the actions offered', () => {
  const ids = (b: GitBranch, c = ctx) => branchActions(b, c).map((a) => a.id);

  it('offer review, merge and push for finished work, and the task and run to open', () => {
    expect(ids(branch())).toEqual(['review', 'merge', 'push', 'task', 'run']);
    expect(branchActions(branch(), ctx)[0]).toMatchObject({ id: 'review', primary: true });
  });

  it('do not offer to push a branch that is already merged: it is the target that has something to push', () => {
    const merged = branch({ merged: true, notPushed: 3, vsTarget: { ahead: 0, behind: 1, relation: 'merged' } });
    expect(ids(merged)).not.toContain('push');
  });

  it('offer a pull request only once the branch is on the remote, and only when there is none', () => {
    const pushed = branch({ upstream: { state: 'in_sync', ahead: 0, behind: 0, name: 'origin/x' }, notPushed: 0 });
    expect(ids(pushed)).toContain('pr');
    expect(ids(pushed, { github: true, hasOpenPR: true })).not.toContain('pr');
    expect(ids(pushed, { github: false, hasOpenPR: false })).not.toContain('pr');
    expect(ids(branch())).not.toContain('pr');
  });

  it('do not offer merge or delete while an agent is working', () => {
    const working = branch({ devboard: { ...branch().devboard, activeRun: true } });
    expect(ids(working)).not.toContain('merge');
    const mergedWorking = branch({ merged: true, vsTarget: { ahead: 0, behind: 1, relation: 'merged' }, devboard: { ...branch().devboard, activeRun: true } });
    expect(ids(mergedWorking)).not.toContain('delete');
    expect(ids(mergedWorking)).not.toContain('clean');
  });

  it('offer cleanup for a merged branch: the worktree first, then the branch', () => {
    const wt = { path: '/w', primary: false, owned: true, worktreeId: 'wt_1' };
    const merged = { merged: true, notPushed: 0, vsTarget: { ahead: 0, behind: 1, relation: 'merged' as const } };
    expect(ids(branch({ ...merged, worktree: wt }))[0]).toBe('clean');
    expect(ids(branch({ ...merged, worktree: wt }))).toContain('delete');
    expect(ids(branch(merged))[0]).toBe('delete');
    expect(branchActions(branch(merged), ctx).find((a) => a.id === 'delete')?.danger).toBe(true);
  });

  it('never offer deletion for what Werkbord did not create, or for protected or target branches', () => {
    const merged = { merged: true, notPushed: 0, vsTarget: { ahead: 0, behind: 1, relation: 'merged' as const } };
    expect(ids(branch({ ...merged, devboard: { created: false, namespace: true, phase: 'none', activeRun: false } }))).not.toContain('delete');
    expect(ids(branch({ ...merged, protected: true }))).not.toContain('delete');
    expect(ids(branch({ target: true, vsTarget: { ahead: 0, behind: 0, relation: 'target' }, merged: true }))).not.toContain('delete');
  });

  it('offer nothing but review for an unusual name, and little for a remote branch', () => {
    expect(ids(branch({ unusual: 'starts with -' }))).toEqual(['review']);
    expect(ids(branch({ scope: 'remote' }))).toEqual(['review']);
    expect(ids(branch({ scope: 'remote', vsTarget: { ahead: 0, behind: 0, relation: 'merged' } }))).toEqual([]);
  });

  it('offer pushing the target when it is ahead of its remote', () => {
    const main = branch({ name: 'main', target: true, devboard: { created: false, namespace: false, phase: 'none', activeRun: false }, vsTarget: { ahead: 0, behind: 0, relation: 'target' }, upstream: { state: 'ahead', ahead: 1, behind: 0, name: 'origin/main' }, notPushed: 1 });
    expect(ids(main)).toEqual(['push']);
  });

  it('keep a card to review plus two changing actions', () => {
    const qa = quickActions(branch({ upstream: { state: 'in_sync', ahead: 0, behind: 0, name: 'origin/x' }, notPushed: 0 }), ctx);
    expect(qa.length).toBeLessThanOrEqual(2);
    expect(qa.map((a) => a.id)).toEqual(['merge', 'pr']);
  });
});

describe('results', () => {
  it('only done is good', () => {
    expect(outcomeTone('done')).toBe('ok');
    for (const o of ['rejected', 'failed', 'auth_failed'] as const) expect(outcomeTone(o)).toBe('bad');
    for (const o of ['conflict', 'unavailable', 'unverified'] as const) expect(outcomeTone(o)).toBe('ask');
    expect(outcomeTitle('unverified')).toBe('Not confirmed');
    expect(outcomeTitle('refused')).toMatch(/not safe/);
  });
});

describe('diffs', () => {
  it('classify lines', () => {
    expect(diffLineKind('@@ -1,3 +1,4 @@')).toBe('hunk');
    expect(diffLineKind('+added')).toBe('add');
    expect(diffLineKind('-removed')).toBe('del');
    expect(diffLineKind(' context')).toBe('ctx');
    expect(diffLineKind('+++ b/file')).toBe('meta');
    expect(diffLineKind('--- a/file')).toBe('meta');
    expect(diffLineKind('diff --git a/x b/x')).toBe('meta');
    expect(diffLineKind('rename from a')).toBe('meta');
    expect(diffLineKind('Binary files a/x and b/x differ')).toBe('meta');
    expect(diffLineKind('')).toBe('ctx');
  });
});

describe('pull requests', () => {
  const pr = (over: Partial<GitHubPR>): GitHubPR => ({
    number: 1, title: 't', url: 'u', state: 'open', draft: false, headBranch: 'x', baseBranch: 'main',
    checks: { state: 'none', total: 0, passed: 0, failed: 0, pending: 0 }, ...over,
  });

  it('shows an unknown mergeability as nothing, never as a yes', () => {
    expect(mergeableLabel(undefined)).toBeNull();
    expect(mergeableLabel('mergeable')?.text).toBe('no conflicts');
    expect(mergeableLabel('conflicting')?.tone).toBe('bad');
  });

  it('indexes open pull requests of this repository by head branch', () => {
    const m = openPRsByBranch([pr({ headBranch: 'a' }), pr({ number: 2, headBranch: 'b', state: 'merged' }), pr({ number: 3, headBranch: 'c', crossRepo: true })]);
    expect([...m.keys()]).toEqual(['a']);
  });
});

describe('links from GitHub', () => {
  it('only http(s) addresses are followed', () => {
    expect(safeURL('https://github.com/a/b/pull/1')).toBe('https://github.com/a/b/pull/1');
    expect(safeURL('http://ghe.corp/x')).toBe('http://ghe.corp/x');
    for (const bad of ['javascript:alert(1)', 'data:text/html,<b>', 'file:///etc/passwd', '', undefined, 'not a url', '//evil.example']) {
      expect(safeURL(bad)).toBe('#');
    }
  });
});
