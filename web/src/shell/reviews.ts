// The window's one Reviews page: everything waiting on a review, from Personal and from every team, in one list. Pure: the workspaces'
// own summaries are what is read (no workspace is asked anything), and each row opens in the workspace it belongs to, where the
// review itself is done.

import type { Entry, Overview, WorkItem } from './types';

export type ReviewKind =
  /** Someone's work (a teammate's ticket, or an agent's finished run) is waiting for the person to look at it. */
  | 'to_review'
  /** A reviewer sent the person's own work back with changes to make. */
  | 'changes'
  /** The person's own work is in review, waiting on someone else. */
  | 'mine';

export interface ReviewRow {
  workspace: Entry;
  id: string;
  kind: ReviewKind;
  title: string;
  project?: string;
  detail?: string;
  href?: string;
  at?: string;
}

export const KIND_LABEL: Record<ReviewKind, string> = {
  to_review: 'To review',
  changes: 'Changes requested',
  mine: 'Your work in review',
};

const rank: Record<ReviewKind, number> = { to_review: 0, changes: 1, mine: 2 };

const inWhere = (e: Entry, where: string): boolean => where === 'all' || e.id === where;

/** Every review in the workspaces that answered, most urgent kind first and newest first within it, for a place (`all`, or a workspace id). */
export function reviewRows(o: Overview, where = 'all'): ReviewRow[] {
  const rows: ReviewRow[] = [];
  for (const e of o.entries) {
    if (!inWhere(e.workspace, where) || !e.summary) continue;
    const taken = new Set<string>();
    for (const a of e.summary.attention) {
      if (a.kind !== 'review' && a.kind !== 'changes_requested') continue;
      if (a.href) taken.add(a.href);
      rows.push({ workspace: e.workspace, id: a.id, kind: a.kind === 'review' ? 'to_review' : 'changes', title: a.title, project: a.project, detail: a.detail, href: a.href, at: a.at });
    }
    for (const w of e.summary.work as WorkItem[]) {
      if (w.status !== 'review' || (w.href && taken.has(w.href))) continue;
      rows.push({ workspace: e.workspace, id: 'work:' + w.id, kind: 'mine', title: w.title, project: w.project, href: w.href, at: w.updatedAt });
    }
  }
  return rows.sort((a, b) => rank[a.kind] - rank[b.kind] || (b.at ?? '').localeCompare(a.at ?? '') || a.title.localeCompare(b.title));
}

/** How many reviews wait on the person: those to review and those sent back. What the sidebar says; their own work in review is not a wait. */
export function reviewCount(o: Overview | null): number {
  return o ? reviewRows(o).filter((r) => r.kind !== 'mine').length : 0;
}
