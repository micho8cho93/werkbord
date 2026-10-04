// Git's screens below `#/p/<id>/git`, and how to link to them. Pure, so it can be tested.
//
//   git                                      the overview: summary, what needs you, pull requests, commits
//   git/branch/<scope>/<name>                one branch: status, actions, commits, changed files
//   git/branch/<scope>/<name>/file?path=…    one file's diff on that branch
//   git/changes[/<worktree id>]              uncommitted work, in the project's checkout or a worktree
//   git/change/<worktree|main>?kind=…&path=… one uncommitted file's diff
//   git/commits/<scope>/<name>               a branch's history
//
// Branch names and paths contain '/', so they are always encoded as a single segment
// (or in the query), and survive any name Git allows.

import type { BranchScope } from './types';

export type GitScreen =
  | { screen: 'overview' }
  | { screen: 'branch'; scope: BranchScope; name: string }
  | { screen: 'file'; scope: BranchScope; name: string; path: string; oldPath: string }
  | { screen: 'changes'; worktree: string }
  | { screen: 'change'; worktree: string; kind: ChangeKind; path: string; oldPath: string }
  | { screen: 'commits'; scope: BranchScope; name: string };

export type ChangeKind = 'staged' | 'unstaged' | 'untracked';

const enc = encodeURIComponent;

function dec(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return '';
  }
}

const isScope = (s: string): s is BranchScope => s === 'local' || s === 'remote';
const isKind = (s: string): s is ChangeKind => s === 'staged' || s === 'unstaged' || s === 'untracked';

/** Reads what follows `git/` in the address. Anything not understood is the overview. */
export function parseGit(sub: string | undefined): GitScreen {
  const overview: GitScreen = { screen: 'overview' };
  if (!sub) return overview;
  const [pathPart, query = ''] = sub.split('?', 2);
  const seg = pathPart.split('/').filter((s) => s !== '');
  const q = new URLSearchParams(query);
  const [kind, a = '', b = '', c = ''] = seg;

  switch (kind) {
    case 'branch': {
      const name = dec(b);
      if (!isScope(a) || !name) return overview;
      if (c === 'file') {
        const path = q.get('path') ?? '';
        return path ? { screen: 'file', scope: a, name, path, oldPath: q.get('old') ?? '' } : { screen: 'branch', scope: a, name };
      }
      return { screen: 'branch', scope: a, name };
    }
    case 'changes':
      return { screen: 'changes', worktree: dec(a) };
    case 'change': {
      const path = q.get('path') ?? '';
      const k = q.get('kind') ?? '';
      if (!path || !isKind(k)) return { screen: 'changes', worktree: a === 'main' ? '' : dec(a) };
      return { screen: 'change', worktree: a === 'main' ? '' : dec(a), kind: k, path, oldPath: q.get('old') ?? '' };
    }
    case 'commits': {
      const name = dec(b);
      return isScope(a) && name ? { screen: 'commits', scope: a, name } : overview;
    }
  }
  return overview;
}

const base = (projectId: string) => `#/p/${enc(projectId)}/git`;

export function overviewHref(projectId: string): string {
  return base(projectId);
}

export function branchHref(projectId: string, scope: BranchScope, name: string): string {
  return `${base(projectId)}/branch/${scope}/${enc(name)}`;
}

export function fileHref(projectId: string, scope: BranchScope, name: string, path: string, oldPath = ''): string {
  const q = new URLSearchParams({ path });
  if (oldPath) q.set('old', oldPath);
  return `${branchHref(projectId, scope, name)}/file?${q}`;
}

export function changesHref(projectId: string, worktreeId = ''): string {
  return worktreeId ? `${base(projectId)}/changes/${enc(worktreeId)}` : `${base(projectId)}/changes`;
}

export function changeHref(projectId: string, worktreeId: string, kind: ChangeKind, path: string, oldPath = ''): string {
  const q = new URLSearchParams({ kind, path });
  if (oldPath) q.set('old', oldPath);
  return `${base(projectId)}/change/${worktreeId ? enc(worktreeId) : 'main'}?${q}`;
}

export function commitsHref(projectId: string, scope: BranchScope, name: string): string {
  return `${base(projectId)}/commits/${scope}/${enc(name)}`;
}

/** Where "back" goes from a screen: one level up in the drill-down. */
export function parentOf(projectId: string, s: GitScreen): string {
  switch (s.screen) {
    case 'overview':
      return overviewHref(projectId);
    case 'branch':
    case 'commits':
      return overviewHref(projectId);
    case 'file':
      return branchHref(projectId, s.scope, s.name);
    case 'changes':
      return overviewHref(projectId);
    case 'change':
      return changesHref(projectId, s.worktree);
  }
}
