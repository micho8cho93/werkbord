// What the Git Control Center says, as words and as choices. Pure functions over the
// controller's data, so the wording is tested once and every screen agrees.
//
// Nothing here decides whether an action is allowed: the controller checks that,
// again, when it is asked. This only decides what to offer and how to put it.

import { timeAgo, type Tone } from './format';
import type {
  ActionOutcome,
  FileStatus,
  GitAttention,
  GitBranch,
  GitChangeCounts,
  GitHubPR,
  GitHubState,
  GitUpstream,
} from './types';

export type { Tone };

export const shortSha = (sha: string | undefined): string => (sha ? sha.slice(0, 8) : '');

/** "devboard/fix-login-3k9x2m" → { prefix: "devboard/", name: "fix-login-3k9x2m" }: the namespace is shown quietly. */
export function splitBranch(name: string): { prefix: string; rest: string } {
  const i = name.lastIndexOf('/');
  return i === -1 ? { prefix: '', rest: name } : { prefix: name.slice(0, i + 1), rest: name.slice(i + 1) };
}

/** "src/lib/a.ts" → { dir: "src/lib/", name: "a.ts" }. */
export function splitPath(path: string): { dir: string; name: string } {
  const i = path.lastIndexOf('/');
  return i === -1 ? { dir: '', name: path } : { dir: path.slice(0, i + 1), name: path.slice(i + 1) };
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

// ---- branches ----

/** How a branch stands against the target, in a few words. */
export function relationLabel(b: GitBranch, targetName: string): string {
  const t = targetName || 'the target';
  const v = b.vsTarget;
  switch (v.relation) {
    case 'target':
      return 'the target branch';
    case 'same':
      return `nothing beyond ${t}`;
    case 'merged':
      return `merged into ${t}`;
    case 'behind':
      return `behind ${t} by ${v.behind}, with no commits of its own`;
    case 'ahead':
      return `${v.ahead} ahead of ${t}`;
    case 'diverged':
      return `diverged from ${t}: ${v.ahead} ahead, ${v.behind} behind`;
    default:
      return 'not compared';
  }
}

/** Where a local branch stands against its upstream. As of the last fetch, which the screens say. */
export function upstreamLabel(u: GitUpstream): string {
  switch (u.state) {
    case 'none':
      return 'no upstream';
    case 'gone':
      return `${u.name ?? 'upstream'} was deleted`;
    case 'in_sync':
      return `in sync with ${u.name}`;
    case 'ahead':
      return `${plural(u.ahead, 'commit')} not pushed`;
    case 'behind':
      return `${plural(u.behind, 'commit')} behind ${u.name}`;
    case 'diverged':
      return `diverged from ${u.name}: ${u.ahead} ahead, ${u.behind} behind`;
  }
}

export interface Chip {
  text: string;
  tone: Tone;
}

/** The few facts a card carries as chips, most important first. */
export function branchChips(b: GitBranch, targetName: string): Chip[] {
  const chips: Chip[] = [];
  if (b.scope === 'remote') chips.push({ text: 'remote', tone: 'neutral' });
  if (b.target) chips.push({ text: 'target', tone: 'ok' });
  const v = b.vsTarget;
  if (!b.target) {
    switch (v.relation) {
      case 'ahead':
        chips.push({ text: `${v.ahead} ahead`, tone: 'work' });
        break;
      case 'diverged':
        chips.push({ text: `${v.ahead}↑ ${v.behind}↓ diverged`, tone: 'ask' });
        break;
      case 'merged':
        chips.push({ text: 'merged', tone: 'ok' });
        break;
      case 'behind':
        chips.push({ text: 'no commits', tone: 'neutral' });
        break;
      case 'same':
        chips.push({ text: 'no commits', tone: 'neutral' });
        break;
    }
  }
  if (b.scope === 'local') {
    switch (b.upstream.state) {
      case 'none':
        chips.push({ text: b.notPushed > 0 && !b.merged ? 'not on any remote' : 'local only', tone: b.notPushed > 0 && !b.merged ? 'ask' : 'neutral' });
        break;
      case 'gone':
        chips.push({ text: 'upstream gone', tone: 'ask' });
        break;
      case 'ahead':
        chips.push({ text: `${b.upstream.ahead} to push`, tone: 'ask' });
        break;
      case 'behind':
        chips.push({ text: `${b.upstream.behind} to pull`, tone: 'neutral' });
        break;
      case 'diverged':
        chips.push({ text: 'diverged from remote', tone: 'ask' });
        break;
    }
  }
  if (b.worktree?.dirty && dirtyTotal(b.worktree.dirty) > 0) {
    chips.push({ text: `${dirtyTotal(b.worktree.dirty)} uncommitted`, tone: 'ask' });
  }
  const agent = agentChip(b.devboard);
  if (agent) chips.push(agent);
  if (b.stale) chips.push({ text: 'stale', tone: 'neutral' });
  if (b.unusual) chips.push({ text: 'unusual name', tone: 'bad' });
  void targetName;
  return chips;
}

/** What a branch's live session is doing: working, waiting for you, or just open and idle. */
export function agentChip(own: GitBranch['devboard']): Chip | null {
  if (!own.activeRun) return null;
  switch (own.runState) {
    case 'waiting_for_user':
      return own.runWaiting === 'question' ? { text: 'needs your answer', tone: 'ask' } : { text: 'session idle', tone: 'neutral' };
    case 'blocked':
      return { text: 'agent blocked', tone: 'block' };
    default:
      return { text: 'agent working', tone: 'work' };
  }
}

export const dirtyTotal = (c: GitChangeCounts): number => c.staged + c.unstaged + c.untracked + c.conflicted;

/** Who made the branch, for the line under its name. */
export function originLabel(b: GitBranch): string {
  if (b.devboard.created) return b.devboard.taskTitle ? `Werkbord · ${b.devboard.taskTitle}` : 'Created by Werkbord';
  if (b.devboard.namespace) return 'Has Werkbord’s name, but Werkbord has no record of making it';
  return b.scope === 'remote' ? 'On the remote only' : 'Not created by Werkbord';
}

export function phaseLabel(p: GitBranch['devboard']['phase']): string {
  return { none: '', active: 'Task in progress', review: 'Task in review', completed: 'Task done', idle: 'Task not started' }[p];
}

/** A local branch with a reason to look at it: something to decide, or something wrong. */
export function needsAttention(b: GitBranch): boolean {
  return b.scope === 'local' && !b.target && b.attention.some((a) => a.severity !== 'info');
}

/** The most pressing reason, for a card's headline. */
export function headline(b: GitBranch): GitAttention | undefined {
  const order = { action: 0, warn: 1, info: 2 } as const;
  return [...b.attention].sort((x, y) => order[x.severity] - order[y.severity])[0];
}

export type BranchFilter = 'attention' | 'devboard' | 'local' | 'remote' | 'merged' | 'all';

export const BRANCH_FILTERS: readonly { id: BranchFilter; label: string }[] = [
  { id: 'attention', label: 'Needs you' },
  { id: 'devboard', label: 'Werkbord' },
  { id: 'local', label: 'Local' },
  { id: 'remote', label: 'Remote' },
  { id: 'merged', label: 'Merged' },
  { id: 'all', label: 'All' },
];

export function filterBranches(branches: readonly GitBranch[], f: BranchFilter): GitBranch[] {
  switch (f) {
    case 'attention':
      return branches.filter(needsAttention);
    case 'devboard':
      return branches.filter((b) => b.devboard.created);
    case 'local':
      return branches.filter((b) => b.scope === 'local');
    case 'remote':
      return branches.filter((b) => b.scope === 'remote');
    case 'merged':
      return branches.filter((b) => b.scope === 'local' && !b.target && b.vsTarget.relation === 'merged');
    default:
      return [...branches];
  }
}

// ---- actions on a branch ----

export type BranchActionId = 'review' | 'merge' | 'push' | 'pr' | 'delete' | 'clean' | 'task' | 'run';

export interface BranchAction {
  id: BranchActionId;
  label: string;
  /** Looks like the one thing to do next. */
  primary?: boolean;
  danger?: boolean;
}

export interface ActionContext {
  /** The project has a GitHub remote the GitHub CLI can reach. */
  github: boolean;
  /** A pull request is already open for the branch. */
  hasOpenPR: boolean;
}

/**
 * The actions worth offering for a branch, in the order to show them. This is what to
 * OFFER: the controller refuses anything unsafe, with the reason, when it is asked.
 * An action that could never apply is left out rather than shown disabled.
 */
export function branchActions(b: GitBranch, ctx: ActionContext): BranchAction[] {
  const out: BranchAction[] = [];
  if (b.scope === 'remote') return b.vsTarget.relation === 'ahead' || b.vsTarget.relation === 'diverged' ? [{ id: 'review', label: 'Review changes', primary: true }] : [];
  if (b.unusual) return [{ id: 'review', label: 'Review changes' }];
  const own = b.devboard;
  const ahead = b.vsTarget.relation === 'ahead' || b.vsTarget.relation === 'diverged';
  // A merged branch has nothing to publish: what is worth pushing then is the target.
  const unpushed = !(b.merged && !b.target) && (b.upstream.state === 'none' || b.upstream.state === 'ahead') ? b.notPushed > 0 : false;

  if (!b.target && ahead) out.push({ id: 'review', label: 'Review changes', primary: true });
  if (own.created && b.merged && !b.target && !own.activeRun) {
    if (b.worktree?.owned) out.push({ id: 'clean', label: 'Clean worktree', primary: true });
    else if (!b.protected) out.push({ id: 'delete', label: 'Delete branch', danger: true, primary: true });
  }
  if (own.created && ahead && !own.activeRun && !b.target) out.push({ id: 'merge', label: 'Merge…' });
  if (unpushed || (b.target && b.upstream.state === 'ahead')) out.push({ id: 'push', label: b.target ? `Push ${b.name}` : 'Push' });
  if (ctx.github && ahead && !b.target && !ctx.hasOpenPR && b.upstream.state !== 'none' && b.upstream.state !== 'gone') out.push({ id: 'pr', label: 'Open pull request' });
  if (own.created && b.merged && b.worktree?.owned && !own.activeRun && !b.protected) out.push({ id: 'delete', label: 'Delete branch', danger: true });
  if (own.created && !b.merged && b.worktree?.owned && !own.activeRun) out.push({ id: 'clean', label: 'Clean worktree', danger: true });
  if (own.taskId) out.push({ id: 'task', label: 'Open task' });
  if (own.runId) out.push({ id: 'run', label: 'Open run' });
  return dedupe(out);
}

function dedupe(actions: BranchAction[]): BranchAction[] {
  const seen = new Set<string>();
  return actions.filter((a) => (seen.has(a.id) ? false : (seen.add(a.id), true)));
}

/** The actions a card shows beside "Review": the ones that change something, at most two. */
export function quickActions(b: GitBranch, ctx: ActionContext): BranchAction[] {
  return branchActions(b, ctx)
    .filter((a) => a.id !== 'review' && a.id !== 'task' && a.id !== 'run')
    .slice(0, 2);
}

// ---- results ----

export function outcomeTone(o: ActionOutcome): Tone {
  switch (o) {
    case 'done':
      return 'ok';
    case 'refused':
    case 'noop':
      return 'neutral';
    case 'conflict':
    case 'unavailable':
    case 'unverified':
      return 'ask';
    default:
      return 'bad';
  }
}

export function outcomeTitle(o: ActionOutcome): string {
  return {
    done: 'Done',
    refused: 'Not done: it was not safe',
    noop: 'Nothing to do',
    conflict: 'Conflict: undone',
    rejected: 'Rejected by the remote',
    unavailable: 'Could not reach it',
    auth_failed: 'Not signed in',
    failed: 'Failed',
    unverified: 'Not confirmed',
  }[o];
}

// ---- files and diffs ----

export function statusGlyph(s: FileStatus): string {
  return { added: 'A', modified: 'M', deleted: 'D', renamed: 'R', copied: 'C', typechange: 'T', conflicted: '!', untracked: '?' }[s];
}

export function statusWord(s: FileStatus): string {
  return { added: 'added', modified: 'modified', deleted: 'deleted', renamed: 'renamed', copied: 'copied', typechange: 'type changed', conflicted: 'in conflict', untracked: 'untracked' }[s];
}

export type DiffLineKind = 'add' | 'del' | 'hunk' | 'meta' | 'ctx';

/** What a line of a unified diff is, for colouring it. */
export function diffLineKind(line: string): DiffLineKind {
  if (line.startsWith('@@')) return 'hunk';
  if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff --git') || line.startsWith('index ') || /^(new|deleted) file mode|^(old|new) mode|^similarity index|^rename (from|to)|^Binary files/.test(line)) return 'meta';
  if (line.startsWith('+')) return 'add';
  if (line.startsWith('-')) return 'del';
  return 'ctx';
}

// ---- remote state ----

/** How old what is known about the remote is. */
export function fetchedLabel(lastFetchedAt: string | undefined, now: number): string {
  return lastFetchedAt ? `fetched ${timeAgo(lastFetchedAt, now)}` : 'never fetched';
}

export function prStateLabel(pr: GitHubPR): { text: string; tone: Tone } {
  if (pr.state === 'merged') return { text: 'Merged', tone: 'ok' };
  if (pr.state === 'closed') return { text: 'Closed', tone: 'neutral' };
  return pr.draft ? { text: 'Draft', tone: 'neutral' } : { text: 'Open', tone: 'work' };
}

export function checksLabel(c: GitHubPR['checks']): { text: string; tone: Tone } | null {
  switch (c.state) {
    case 'passing':
      return { text: `checks passing (${c.passed})`, tone: 'ok' };
    case 'failing':
      return { text: `${c.failed} check${c.failed === 1 ? '' : 's'} failing`, tone: 'bad' };
    case 'pending':
      return { text: `${c.pending} check${c.pending === 1 ? '' : 's'} running`, tone: 'ask' };
    default:
      return null;
  }
}

export function reviewLabel(r: GitHubPR['review']): { text: string; tone: Tone } | null {
  switch (r) {
    case 'approved':
      return { text: 'approved', tone: 'ok' };
    case 'changes_requested':
      return { text: 'changes requested', tone: 'bad' };
    case 'review_required':
      return { text: 'review required', tone: 'ask' };
    default:
      return null;
  }
}

/** GitHub often has not worked out mergeability yet; an unknown is not shown at all rather than as a yes. */
export function mergeableLabel(m: GitHubPR['mergeable']): { text: string; tone: Tone } | null {
  switch (m) {
    case 'mergeable':
      return { text: 'no conflicts', tone: 'ok' };
    case 'conflicting':
      return { text: 'has conflicts', tone: 'bad' };
    default:
      return null;
  }
}

/** Why there are no pull requests, in a sentence. */
export function githubUnavailable(s: GitHubState): string {
  switch (s.reason) {
    case 'no_remote':
      return 'This repository has no remote, so there are no pull requests.';
    case 'not_github':
      return 'The remote does not look like a GitHub repository.';
    case 'gh_missing':
      return s.message || 'The GitHub CLI (gh) is not installed. Everything local still works.';
    case 'unauthenticated':
      return 'The GitHub CLI is not signed in. Run `gh auth login` on this computer. Everything local still works.';
    default:
      return `GitHub could not be asked: ${s.message || 'unknown error'}. Everything local still works.`;
  }
}

/** Open pull requests by head branch, to offer "Open pull request" only where there is none. */
export function openPRsByBranch(prs: readonly GitHubPR[]): Map<string, GitHubPR> {
  const m = new Map<string, GitHubPR>();
  for (const pr of prs) if (pr.state === 'open' && !pr.crossRepo) m.set(pr.headBranch, pr);
  return m;
}

/** A link from GitHub's data is only followed if it is an http(s) address: data is never a way to run a script. */
export function safeURL(u: string | undefined): string {
  try {
    const url = new URL(u ?? '');
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : '#';
  } catch {
    return '#';
  }
}
