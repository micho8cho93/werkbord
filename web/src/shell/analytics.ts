// The window's one Analytics page: what each workspace reports about its own work, put side by side. Pure: the app reads each
// workspace (`Analytics`), these functions turn what came back into one shape, merge the parts the person selected, and offer the
// filters. Nothing here asks a workspace anything.
//
// What the two kinds of workspace report is not the same, and is not made to look it. Both report the work itself (tasks created and
// moved to Done, how long they took, by day and by category), so those merge. Only Personal reports what its own agents did (runs,
// tokens, cost): a team records coordination only, by design, so that part is Personal's alone.

import { executionTotals, type AnalyticsReport, type AnalyticsExecution } from '../lib/analytics';
import { listed, sectionItems } from './nav';
import type { Item, Kind, Overview } from './types';

export const RANGES = [7, 30, 90] as const;

export interface Day {
  date: string;
  created: number;
  completed: number;
}

/** The work itself, as one workspace or several together report it. */
export interface Activity {
  created: number;
  completed: number;
  durationSamples: number;
  averageSeconds: number | null;
  /** A median cannot be merged: it is only known when one workspace's figures are shown. */
  medianSeconds: number | null;
  days: Day[];
  categories: { name: string; created: number; completed: number }[];
  /** A workspace's bounded read left older records out. */
  partial: boolean;
  end: string;
}

const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const opt = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

/** One workspace's report as an Activity. Personal and a team name a few fields differently; anything missing is zero or unknown. */
export function activityOf(raw: unknown): Activity {
  const r = obj(raw);
  const days: Day[] = (Array.isArray(r.days) ? r.days : []).map((d) => ({ date: String(obj(d).date ?? ''), created: num(obj(d).created), completed: num(obj(d).completed) })).filter((d) => d.date);
  const categories = (Array.isArray(r.categories) ? r.categories : []).map((c) => ({ name: String(obj(c).name ?? ''), created: num(obj(c).created), completed: num(obj(c).completed) })).filter((c) => c.name);
  return {
    created: num(r.created),
    completed: num(r.completed),
    durationSamples: num(r.durationSamples),
    averageSeconds: opt(r.averageTaskSeconds ?? r.averageTicketSeconds),
    medianSeconds: opt(r.medianTaskSeconds ?? r.medianTicketSeconds),
    days,
    categories,
    partial: r.truncated === true || r.retentionRemoved === true || num(r.missingTaskHistory) > 0,
    end: typeof r.end === 'string' ? r.end : '',
  };
}

/** The Activity of several workspaces together: counts add, days add by date, categories by name, the average is weighted by its samples. */
export function mergeActivity(parts: readonly Activity[]): Activity {
  if (parts.length === 1) return parts[0];
  const days = new Map<string, Day>();
  const categories = new Map<string, { name: string; created: number; completed: number }>();
  let samples = 0;
  let weighted = 0;
  for (const p of parts) {
    for (const d of p.days) {
      const at = days.get(d.date) ?? { date: d.date, created: 0, completed: 0 };
      at.created += d.created;
      at.completed += d.completed;
      days.set(d.date, at);
    }
    for (const c of p.categories) {
      const key = c.name.toLowerCase();
      const at = categories.get(key) ?? { name: c.name, created: 0, completed: 0 };
      at.created += c.created;
      at.completed += c.completed;
      categories.set(key, at);
    }
    if (p.averageSeconds !== null && p.durationSamples > 0) {
      samples += p.durationSamples;
      weighted += p.averageSeconds * p.durationSamples;
    }
  }
  return {
    created: parts.reduce((n, p) => n + p.created, 0),
    completed: parts.reduce((n, p) => n + p.completed, 0),
    durationSamples: parts.reduce((n, p) => n + p.durationSamples, 0),
    averageSeconds: samples > 0 ? weighted / samples : null,
    medianSeconds: null,
    days: [...days.values()].sort((a, b) => a.date.localeCompare(b.date)),
    categories: [...categories.values()].sort((a, b) => b.completed - a.completed || b.created - a.created || a.name.localeCompare(b.name)),
    partial: parts.some((p) => p.partial),
    end: parts.map((p) => p.end).sort().at(-1) ?? '',
  };
}

/** What Personal's agents did, from its report: runs, how they ended, tokens and cost (unknown stays unknown, never zero). */
export function runsOf(raw: unknown) {
  const r = obj(raw);
  const rows = (Array.isArray(r.executions) ? r.executions : []) as AnalyticsExecution[];
  const totals = executionTotals(rows);
  return {
    totals,
    rows,
    days: (Array.isArray(r.days) ? r.days : []) as AnalyticsReport['days'],
    providers: [...rows.reduce((m, row) => m.set(row.provider, (m.get(row.provider) ?? 0) + row.attempts), new Map<string, number>())].map(([name, attempts]) => ({ name, attempts })).sort((a, b) => b.attempts - a.attempts),
    accepted: num(r.accepted),
    rejected: num(r.rejected),
    verifiedMerges: num(r.verifiedMerges),
    normalSessionCompletions: num(r.normalSessionCompletions),
    blocked: num(r.blocked),
    stopped: num(r.stopped),
  };
}

// ---- the filters ----

export interface Where {
  /** `all`, `personal`, or a team's workspace id. */
  id: string;
  label: string;
  kind?: Kind;
}

/** Where the person can look: everything, Personal alone, or one team. Only workspaces that can answer are offered. */
export function wheres(items: readonly Item[] | undefined): Where[] {
  const ready = sectionItems(items).filter((i) => i.state === 'ready');
  const out: Where[] = [{ id: 'all', label: 'Personal and all teams' }];
  for (const i of ready) out.push({ id: i.id, label: i.kind === 'personal' ? 'Personal only' : i.name, kind: i.kind });
  return out;
}

export interface ProjectChoice {
  /** The workspace it belongs to, and its own id there. */
  workspace: string;
  id: string;
  name: string;
}

/** The projects that can be picked within a place: those of the workspace chosen, or of every workspace that can answer. */
export function projectsWithin(where: string, items: readonly Item[] | undefined, overview: Overview | null): ProjectChoice[] {
  const ids = new Set(wheres(items).filter((w) => w.id !== 'all').map((w) => w.id));
  const out: ProjectChoice[] = [];
  for (const e of overview?.entries ?? []) {
    if (!ids.has(e.workspace.id) || (where !== 'all' && e.workspace.id !== where)) continue;
    for (const p of listed(e.summary?.projects)) out.push({ workspace: e.workspace.id, id: p.id, name: p.name });
  }
  return out;
}

/** What to read for a choice of place and project: the workspaces, and the project in each (one project is one workspace's). */
export function reads(where: string, project: ProjectChoice | undefined, items: readonly Item[] | undefined): { workspace: string; project: string }[] {
  if (project) return [{ workspace: project.workspace, project: project.id }];
  return wheres(items).filter((w) => w.id !== 'all' && (where === 'all' || w.id === where)).map((w) => ({ workspace: w.id, project: '' }));
}

/** A question the page can say it cannot answer for a choice: what Personal alone reports is not there when only teams are read. */
export function runsAvailable(readFrom: readonly { workspace: string }[]): boolean {
  return readFrom.some((r) => r.workspace === 'personal');
}
