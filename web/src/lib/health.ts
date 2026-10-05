// What repository health says, as words and as choices. Pure functions over the controller's
// findings, so the wording is tested once and every screen agrees.
//
// Nothing here runs anything. A finding recommends a next step; this only says how to offer
// it. Every step that changes the repository opens the same confirmation sheet a person would
// reach from the branch list, and the controller checks again when it is confirmed.

import { changesHref, branchHref } from './gitroute';
import type { Tone } from './format';
import type { HealthBasis, HealthFinding, HealthSeverity, RepositoryHealth } from './types';

const RANK: Record<HealthSeverity, number> = { info: 1, attention: 2, risk: 3, critical: 4 };

/** Orders severities, lowest first. */
export const severityRank = (s: HealthSeverity): number => RANK[s] ?? 0;

/** How a severity reads. "info" is housekeeping: listed, never counted as needing attention. */
export function severityLabel(s: HealthSeverity): string {
  switch (s) {
    case 'critical':
      return 'Critical';
    case 'risk':
      return 'Risk';
    case 'attention':
      return 'Needs attention';
    default:
      return 'Housekeeping';
  }
}

export function severityTone(s: HealthSeverity): Tone {
  switch (s) {
    case 'critical':
      return 'bad';
    case 'risk':
      return 'block';
    case 'attention':
      return 'ask';
    default:
      return 'neutral';
  }
}

/** Whether a finding is something to act on: above housekeeping. */
export const needsAttention = (f: HealthFinding): boolean => severityRank(f.severity) >= RANK.attention;

/**
 * Says how sure a signal is, in words a person can weigh. A fact is stated by Git or Dev
 * Board's records; a guess is a pattern, and the finding is worded as a possibility.
 */
export function basisLabel(b: HealthBasis): string {
  return b === 'deterministic' ? 'Git says so' : 'A guess';
}

export function basisHint(b: HealthBasis): string {
  return b === 'deterministic'
    ? 'This comes from Git metadata or Werkbord’s own records: it is a fact about the repository as of the last check.'
    : 'This is inferred from a pattern, not proven by Git. It may be wrong; look before acting on it.';
}

/** The findings that need a person, and the housekeeping, separately. */
export function splitFindings(r: RepositoryHealth | null | undefined): { needs: HealthFinding[]; housekeeping: HealthFinding[] } {
  const all = r?.findings ?? [];
  return { needs: all.filter(needsAttention), housekeeping: all.filter((f) => !needsAttention(f)) };
}

/** The tone of the headline: the worst thing that is open. */
export function headlineTone(r: RepositoryHealth | null | undefined): Tone {
  switch (r?.summary.state) {
    case 'critical':
      return 'bad';
    case 'risk':
      return 'block';
    case 'attention':
      return 'ask';
    case 'healthy':
      return 'ok';
    default:
      return 'neutral';
  }
}

/** "2 housekeeping items", or empty when there are none. */
export function housekeepingLine(n: number): string {
  if (n <= 0) return '';
  return n === 1 ? '1 housekeeping item' : `${n} housekeeping items`;
}

/** What each kind of finding is called in a short label, for the Control Center and the filters. */
export function categoryLabel(c: HealthFinding['category']): string {
  switch (c) {
    case 'uncommitted':
      return 'Uncommitted work';
    case 'unsynced':
      return 'Unsynced work';
    case 'branch':
      return 'Branch';
    case 'worktree':
      return 'Worktree';
    case 'orchestration':
      return 'Agents';
    case 'operation':
      return 'Git operation';
  }
}

// ---- turning a recommended action into something to tap ----

/**
 * What tapping a finding's action does. Only `sheet` and `clean` can change the repository,
 * and both open a confirmation that checks again; `task` adds a card to the board and starts
 * nothing; the rest read, navigate or tell the person what to do themselves.
 */
export type ActionPlan =
  | { type: 'sheet'; sheet: 'push' | 'merge' | 'delete'; branch: string }
  | { type: 'clean'; worktreeId: string; branch: string }
  | { type: 'link'; href: string }
  | { type: 'task'; title: string; description: string; askAgent: boolean }
  | { type: 'fetch' }
  /** Werkbord cannot do this: the reason says why and what to do instead. */
  | { type: 'manual'; reason: string; detail: string };

export function actionPlan(f: HealthFinding, projectId: string): ActionPlan {
  const a = f.action;
  const manual = (): ActionPlan => ({ type: 'manual', reason: a.reason ?? 'Werkbord cannot do this for you.', detail: a.detail ?? '' });
  if (!a.canPerform) return manual();
  switch (a.kind) {
    case 'push_branch':
      return a.branch ? { type: 'sheet', sheet: 'push', branch: a.branch } : manual();
    case 'merge_branch':
      return a.branch ? { type: 'sheet', sheet: 'merge', branch: a.branch } : manual();
    case 'delete_branch':
      return a.branch ? { type: 'sheet', sheet: 'delete', branch: a.branch } : manual();
    case 'clean_worktree':
      return a.worktreeId ? { type: 'clean', worktreeId: a.worktreeId, branch: a.branch ?? f.subject.branch ?? '' } : manual();
    case 'review_changes':
      if (a.worktreeId) return { type: 'link', href: changesHref(projectId, a.worktreeId) };
      if (a.branch) return { type: 'link', href: branchHref(projectId, 'local', a.branch) };
      return { type: 'link', href: changesHref(projectId) };
    case 'fetch':
      return { type: 'fetch' };
    case 'create_task':
    case 'ask_agent':
      return { type: 'task', title: a.taskTitle ?? f.title, description: taskDescription(f), askAgent: a.kind === 'ask_agent' };
    default:
      return manual();
  }
}

/** The description of a task made from a finding: what was seen and what to check, with the evidence. */
export function taskDescription(f: HealthFinding): string {
  const lines = [f.action.taskDescription ?? f.explanation, '', 'Repository health context:', `- Finding: ${f.title}`, `- Confidence: ${f.basis === 'deterministic' ? 'deterministic Git or Werkbord evidence' : 'heuristic; verify before acting'}`];
  if (f.subject.branch) lines.push(`- Branch: ${f.subject.branch}`);
  if (f.subject.worktreePath) lines.push(`- Affected worktree: ${f.subject.worktreePath}`);
  if (f.subject.taskTitle) lines.push(`- Related task: ${f.subject.taskTitle}${f.subject.taskId ? ` (${f.subject.taskId})` : ''}`);
  if (f.subject.runId) lines.push(`- Related run: ${f.subject.runId}`);
  if (f.subject.related?.length) lines.push(`- Related branches: ${f.subject.related.join(', ')}`);
  lines.push('', 'Evidence:');
  for (const e of f.evidence) lines.push(`- ${e.label}: ${e.value}`);
  lines.push('', `Recommended outcome: ${f.action.detail || f.action.label}.`, 'Inspect the current repository state before proposing changes. Do not merge to the default branch automatically.');
  return lines.join('\n');
}

/** The button label for an action: what it does, and whether it is a step Werkbord will take (after confirmation). */
export function actionLabel(f: HealthFinding): string {
  return f.action.label;
}

/** Whether an action's button should look dangerous: it removes something, after a confirmation. */
export const isDestructive = (f: HealthFinding): boolean => !!f.action.destructive && f.action.canPerform;

/** The line under a finding that says when it was first seen. */
export function detectedLabel(f: HealthFinding, now: number, ago: (iso: string, now: number) => string): string {
  return `First seen ${ago(f.detectedAt, now)}`;
}

/** Findings of the same type and project, for showing one count rather than a list: the Control Center groups by project. */
export function byProject(findings: readonly HealthFinding[]): Map<string, HealthFinding[]> {
  const out = new Map<string, HealthFinding[]>();
  for (const f of findings) {
    const list = out.get(f.projectId);
    if (list) list.push(f);
    else out.set(f.projectId, [f]);
  }
  return out;
}
