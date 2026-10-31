// The decisions agents are waiting on a person for, as one first-come, first-served line. This is deliberately not attention.ts's or
// aggregate.ts's order: failures, warnings and notices stay ordered by severity there. Only an agent's question, which blocks the agent
// until answered, is taken in the order it arrived. Pure, so the order, the ties and the stale states can be tested on their own.

import type { Attention, Entry, InboxQuestion, Overview, OverviewEntry } from './types';
import type { ProjectIdentity } from './project-context';
import { projectInHref } from './project-context';

export interface Decision {
  /** Stable across refreshes and reconnects: the workspace and the id its summary gave the question. */
  key: string;
  workspace: Entry;
  /** What the question asks. */
  prompt: string;
  /** When it first arrived. Never when it was last reported. */
  askedAt: string;
  task: string;
  project: string;
  /** The project's own id inside its workspace, when the record names one; what the project context filters on. */
  projectId: string | null;
  agent: string;
  runId?: string;
  href?: string;
  question?: InboxQuestion;
}

export const decisionKey = (workspace: string, attentionId: string): string => `${workspace}|${attentionId}`;

/** A coding agent's identifier, said as a name: `claude-code` is Claude Code. */
export function agentName(id = ''): string {
  return id.split(/[-_\s]+/).filter(Boolean).map((w) => w[0].toUpperCase() + w.slice(1)).join(' ');
}

/** Only what an agent is waiting to be told is a decision. A task that merely needs attention (blocked, failed, in review) is not. */
export const isDecision = (a: Attention): boolean => a.kind === 'needs_input';

function decisionsOf(e: OverviewEntry): Decision[] {
  return (e.summary?.attention ?? []).filter(isDecision).map((a) => ({
    key: decisionKey(e.workspace.id, a.id),
    workspace: e.workspace,
    prompt: a.question?.prompt ?? a.title,
    askedAt: a.question?.askedAt ?? a.at ?? '',
    task: a.detail ?? '',
    project: a.project ?? '',
    projectId: a.question?.projectId ?? projectInHref(e.workspace.kind, a.href),
    agent: agentName(a.question?.agent),
    runId: a.question?.runId,
    href: a.href,
    question: a.question,
  }));
}

/** A workspace that answered, in this overview, with its records: the only thing that may say a decision is gone. */
export const answering = (e: OverviewEntry): boolean => !!e.summary && !e.error && e.workspace.state === 'ready';

/**
 * The queue after an overview arrives. A workspace that answered is the truth about itself: what it no longer lists was resolved (or
 * withdrawn), what it lists is current. A workspace that did not answer (offline, refused, a summary that failed) says nothing, so what
 * was waiting on it stays, to be shown as last reported. A workspace the person has left is gone, with its decisions.
 */
export function reconcile(previous: readonly Decision[], overview: Overview | null): Decision[] {
  if (!overview) return [...previous];
  const out: Decision[] = [];
  for (const e of overview.entries) {
    if (answering(e)) out.push(...decisionsOf(e));
    else out.push(...previous.filter((d) => d.workspace.id === e.workspace.id));
  }
  return out;
}

const time = (at: string): number => {
  const t = Date.parse(at);
  return Number.isFinite(t) ? t : Infinity;
};

/** Oldest arrival first. Two that arrived together (or without a time) are told apart by their key, so the order never flickers. */
export function byArrival(a: Decision, b: Decision): number {
  const d = time(a.askedAt) - time(b.askedAt);
  if (d) return d;
  return a.key < b.key ? -1 : a.key > b.key ? 1 : 0;
}

/** The decisions in the person's project context (all of them with none), minus those they have just answered, in the order to take them. */
export function pending(queue: readonly Decision[], context: ProjectIdentity | null, answered: ReadonlySet<string> = new Set()): Decision[] {
  return queue
    .filter((d) => !answered.has(d.key) && (!context || (d.workspace.id === context.workspace && d.projectId === context.project)))
    .sort(byArrival);
}

/** Whether the workspace behind a decision can be asked now. A decision whose workspace is silent is shown as last reported, not hidden. */
export function current(d: Decision, overview: Overview | null): boolean {
  const e = overview?.entries.find((x) => x.workspace.id === d.workspace.id);
  return !!e && answering(e);
}
