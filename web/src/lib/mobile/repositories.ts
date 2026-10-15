// What the Git screen says about each repository, from what the Git Control Center already holds for it: the local picture, GitHub's
// pull requests (when GitHub is reachable) and the repository's health. Every number is one of those, never worked out here. No
// framework code, so it can be tested on its own.

import { headline, needsAttention } from '../gitui';
import type { GitBranch, GitCommit, GitHubPR, GitHubState, GitOverview, RepositoryHealth } from '../types';

export interface RepoDigest {
  branch: string;
  detached: boolean;
  /** Files with changes not yet committed, counting conflicts. */
  changed: number;
  conflicted: number;
  ahead: number;
  behind: number;
  /** Local branches that want the person: Werkbord's own, unmerged, or unpushed. */
  branchesNeedingYou: number;
  /** Open (not merged, not closed) pull requests; absent when GitHub could not be asked. */
  openPRs: GitHubPR[] | null;
  /** Findings that are not housekeeping. */
  findings: number;
  /** How bad the worst thing is: what the card's mark says. */
  tone: 'ok' | 'ask' | 'bad' | 'neutral';
  /** One line for the card: the health headline, or that nothing is wrong, or that it has not been looked at yet. */
  line: string;
}

export function repoDigest(o: GitOverview | null, github: GitHubState | null, health: RepositoryHealth | null): RepoDigest | null {
  if (!o) return null;
  const w = o.local.workingTree;
  const changed = w.staged.length + w.unstaged.length + w.untracked.length + w.conflicted.length;
  const openPRs = github?.available ? github.pullRequests.filter((p) => p.state === 'open') : null;
  const counts = health?.summary.counts;
  const findings = counts ? counts.attention + counts.risk + counts.critical : 0;
  const tone: RepoDigest['tone'] = !health ? 'neutral' : counts && (counts.critical || counts.risk) ? 'bad' : findings || w.conflicted.length ? 'ask' : 'ok';
  return {
    branch: o.local.head.detached ? `detached at ${o.local.head.commit.slice(0, 7)}` : o.local.head.branch,
    detached: o.local.head.detached,
    changed,
    conflicted: w.conflicted.length,
    ahead: w.ahead,
    behind: w.behind,
    branchesNeedingYou: o.summary.needsYou,
    openPRs,
    findings,
    tone,
    line: health ? health.summary.headline : 'Not checked yet',
  };
}

/** "3 changed · 2 ahead · 1 behind", only what is true; "clean" when nothing is. */
export function workingLine(d: Pick<RepoDigest, 'changed' | 'ahead' | 'behind'>): string {
  const parts = [d.changed ? `${d.changed} changed` : '', d.ahead ? `${d.ahead} ahead` : '', d.behind ? `${d.behind} behind` : ''].filter(Boolean);
  return parts.length ? parts.join(' · ') : 'Clean';
}

export interface RepoSource {
  project: { id: string; name: string };
  overview: GitOverview | null;
  github: GitHubState | null;
}

export type RepoView = 'overview' | 'prs' | 'branches' | 'commits';

/** Open pull requests in every repository GitHub could be asked about, the most recently touched first. */
export function openPullRequests(sources: readonly RepoSource[]): { project: RepoSource['project']; pr: GitHubPR }[] {
  return sources
    .flatMap((s) => (s.github?.available ? s.github.pullRequests.filter((p) => p.state === 'open').map((pr) => ({ project: s.project, pr })) : []))
    .sort((a, b) => (b.pr.updatedAt ?? b.pr.createdAt ?? '').localeCompare(a.pr.updatedAt ?? a.pr.createdAt ?? ''));
}

/** Local branches that want the person (Git's own attention list, minus housekeeping), with the most pressing reason. */
export function branchesNeedingYou(sources: readonly RepoSource[]): { project: RepoSource['project']; branch: GitBranch; reason: string }[] {
  return sources.flatMap((s) => (s.overview?.branches ?? []).filter(needsAttention).map((branch) => ({ project: s.project, branch, reason: headline(branch)?.message ?? '' })));
}

/** The latest commits of every repository, newest first. */
export function latestCommits(sources: readonly RepoSource[], limit = 20): { project: RepoSource['project']; commit: GitCommit }[] {
  return sources
    .flatMap((s) => (s.overview?.local.recentCommits ?? []).map((commit) => ({ project: s.project, commit })))
    .sort((a, b) => b.commit.date.localeCompare(a.commit.date))
    .slice(0, limit);
}
