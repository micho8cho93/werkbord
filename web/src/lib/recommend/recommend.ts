// How a suggestion is worded and where it leads. Pure, so that it can be tested and used by every screen that shows one.

import { branchHref, changesHref, overviewHref } from '../gitroute';
import { globalHref, projectHref, taskHref } from '../location';
import type { RecBasis, RecEvidence, RecIntent, RecTier, Recommendation, RecommendationSet } from './types';

export const INTENT_LABEL: Record<RecIntent, string> = {
  continue_work: 'Continue',
  prepare_integration: 'Branch',
  review_issues: 'Repository',
  find_bugs: 'Find Bugs',
  refactor_codebase: 'Refactor',
  next_tasks: 'Next up',
};

/** Why it was made, in the fewest words. Required comes first because something outranks a choice. */
export const BASIS_LABEL: Record<RecBasis, string> = {
  required: 'Needed first',
  evidence: 'From current state',
  preference: 'From how you work',
};

export const BASIS_TONE: Record<RecBasis, 'ask' | 'work' | 'idle'> = { required: 'ask', evidence: 'work', preference: 'idle' };

/** The five rungs, strongest first, as a person would name them. */
export const TIER_LABEL: Record<RecTier, string> = {
  1: 'App limit',
  2: 'Policy',
  3: 'Your decision',
  4: 'Current state',
  5: 'Habit',
};

const SOURCE_LABEL: Record<string, string> = {
  application: 'Werkbord',
  git: 'Git',
  github: 'GitHub',
  task: 'Task',
  run: 'Run',
  health: 'Repository health',
  individual_memory: 'Your memory',
  team_memory: 'Team memory',
  feedback: 'Your feedback',
};

export const sourceLabel = (s: string): string => SOURCE_LABEL[s] ?? s.replaceAll('_', ' ');

export const CONFIDENCE_LABEL = { high: 'Everything here is verified and current', medium: 'Part of this is inferred from how you work', low: 'The evidence is thin' } as const;

/** A connected Team workspace's project is not on this computer: its screens belong to that workspace. */
export const isConnectedProject = (projectId: string): boolean => projectId.startsWith('workspace:');

export type Destination = { href: string } | { workflow: string } | null;

/**
 * Where taking a suggestion up leads. Always a screen that already exists, where the person decides; a workflow is shown on the
 * Control Deck, never started from here. A connected workspace's project has no screen on this computer.
 */
export function destination(rec: Pick<Recommendation, 'projectId' | 'action'>): Destination {
  const { action, projectId } = rec;
  if (isConnectedProject(projectId)) return null;
  switch (action.kind) {
    case 'open_workflow':
      return action.workflow ? { workflow: action.workflow } : null;
    case 'open_task':
      return action.taskId ? { href: taskHref(projectId, action.taskId, action.runId ?? '') } : null;
    case 'open_branch':
      return { href: action.branch ? branchHref(projectId, 'local', action.branch) : overviewHref(projectId) };
    case 'open_changes':
      return { href: changesHref(projectId) };
    case 'open_git':
    case 'open_health':
      return { href: overviewHref(projectId) };
    case 'open_board':
      return { href: projectHref(projectId, 'board') };
  }
  return null;
}

/** The address of the Control Deck with a workflow pointed at, for a screen that is not the deck. */
export const deckHref = (workflow: string): string => `${globalHref('deck')}?suggest=${encodeURIComponent(workflow)}`;

/** The workflow a deck address points at, or empty. Only a known id is honoured. */
export function suggestedWorkflow(hash: string): string {
  const q = hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : '';
  const v = new URLSearchParams(q).get('suggest') ?? '';
  return v === 'find-bugs' || v === 'refactor-codebase' ? v : '';
}

/** What the person is told about the screen a suggestion opens. Honest about who decides. */
export function confirmationNote(rec: Pick<Recommendation, 'action'>): string {
  if (rec.action.kind === 'open_workflow') return 'You start it yourself from the Control Deck.';
  return rec.action.confirmation === 'at_destination' ? 'Nothing changes until you confirm there.' : '';
}

/** Groups what a suggestion rests on by how strongly it counts, strongest first, for the "Why this" disclosure. */
export function groupedByTier<T extends { tier: RecTier }>(items: readonly T[]): { tier: RecTier; label: string; items: T[] }[] {
  const out = new Map<RecTier, T[]>();
  for (const it of items) out.set(it.tier, [...(out.get(it.tier) ?? []), it]);
  return [...out.entries()].sort(([a], [b]) => a - b).map(([tier, list]) => ({ tier, label: TIER_LABEL[tier], items: list }));
}

/** What the sources could not say, as short notes. A gap is shown; it is never read as "nothing there". */
export function gaps(set: Pick<RecommendationSet, 'sources' | 'notes'>): string[] {
  const out = [...set.notes];
  if (set.sources.teamMemory === 'unavailable') out.push('Team policies could not be read just now.');
  if (set.sources.teamMemory === 'not_checked') out.push('Team policies were not read here.');
  return out;
}

/** The small print under one fact: where it came from, which record, and what limits it. */
export function evidenceMeta(e: Pick<RecEvidence, 'source' | 'ref' | 'verified' | 'note'>): string {
  return [sourceLabel(e.source), e.ref, e.verified ? '' : 'not verified', e.note].filter(Boolean).join(' · ');
}

export function feedbackNote(storedIn: 'memory' | 'device' | undefined): string {
  return storedIn === 'device'
    ? 'Kept on this device only. It shapes what this screen shows and nothing else.'
    : 'Saved as a private preference in your memory on this computer. It only orders suggestions: it never becomes a rule and is never shared with Team.';
}

/** The project last visited in this browser, which is where the assistant's opening suggestions are about. Empty when none. */
export function lastProject(): string {
  try {
    return localStorage.getItem('devboard.project') ?? '';
  } catch {
    return '';
  }
}
