// What needs the person, across every workspace they can open. Pure: the app hands the shell each workspace's summary and
// these functions put them side by side. Nothing here asks a workspace anything, so a workspace that does not answer is a
// row that says so, never a reason another workspace's work is missing.

import type { Attention, Entry, Overview, OverviewEntry, Scheduled, Severity, WorkItem } from './types';

export interface Labeled<T> {
  workspace: Entry;
  item: T;
}

const rank: Record<Severity, number> = { critical: 0, warning: 1, info: 2 };

const ok = (e: OverviewEntry): boolean => !!e.summary;

/** The person's own work, grouped by workspace in the order the switcher lists them. Workspaces with none are left out. */
export function myWork(o: Overview): { workspace: Entry; items: WorkItem[]; error?: string }[] {
  return o.entries
    .map((e) => ({ workspace: e.workspace, items: e.summary?.work ?? [], error: e.error }))
    .filter((g) => g.items.length > 0 || g.error);
}

/** Work that is being done by an agent right now, anywhere. */
export function running(o: Overview): number {
  return o.entries.reduce((n, e) => n + (e.summary?.work.filter((w) => w.execution === 'running' || w.execution === 'queued').length ?? 0), 0);
}

/** Everything that waits for the person, worst first, then newest. */
export function attention(o: Overview): Labeled<Attention>[] {
  const rows: Labeled<Attention>[] = [];
  for (const e of o.entries) for (const a of e.summary?.attention ?? []) rows.push({ workspace: e.workspace, item: a });
  return rows.sort((a, b) => rank[a.item.severity] - rank[b.item.severity] || (b.item.at ?? '').localeCompare(a.item.at ?? ''));
}

export interface Counts {
  /** Things that wait for the person (warnings and worse). */
  needsYou: number;
  critical: number;
  /** Workspaces that did not answer. */
  unreachable: number;
}

export function counts(o: Overview): Counts {
  const rows = attention(o);
  return {
    needsYou: rows.filter((r) => r.item.severity !== 'info').length,
    critical: rows.filter((r) => r.item.severity === 'critical').length,
    unreachable: o.entries.filter((e) => e.error || (!ok(e) && (e.workspace.state === 'offline' || e.workspace.state === 'unavailable'))).length,
  };
}

export interface Day {
  /** The day in the person's own time zone, YYYY-MM-DD. */
  day: string;
  items: Labeled<Scheduled>[];
}

const dayOf = (t: Date): string => {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${t.getFullYear()}-${p(t.getMonth() + 1)}-${p(t.getDate())}`;
};

/** Scheduled work from every workspace, by day in the person's own time zone, soonest first. */
export function agenda(o: Overview, from: Date = new Date(0)): Day[] {
  const rows: Labeled<Scheduled>[] = [];
  for (const e of o.entries) for (const s of e.summary?.schedule ?? []) if (new Date(s.at) >= from) rows.push({ workspace: e.workspace, item: s });
  rows.sort((a, b) => a.item.at.localeCompare(b.item.at));
  const days: Day[] = [];
  for (const r of rows) {
    const d = dayOf(new Date(r.item.at));
    const last = days[days.length - 1];
    if (last && last.day === d) last.items.push(r);
    else days.push({ day: d, items: [r] });
  }
  return days;
}

/** Where a workspace stands, in a sentence, for a place the person looks to find out whether to worry. */
export function standing(e: OverviewEntry): string {
  if (e.error) return e.error;
  const s = e.workspace.state;
  if (s === 'ready') return 'Working';
  return e.workspace.detail || { connecting: 'Connecting', offline: 'Offline', setup: 'Not set up', leaving: 'Leaving', unavailable: 'Unavailable', ready: 'Working' }[s];
}

/** The hosting facts a Team workspace reported, for the one place that explains them. */
export function hosting(e: OverviewEntry): { hosts: string; writable: boolean; warnings: string[] } | null {
  const i = e.summary?.infra;
  if (!i) return null;
  return {
    hosts: i.hostsConfigured > 0 ? `${i.hostsOnline} of ${i.hostsConfigured} Workspace Hosts online` : 'No Workspace Host reported',
    writable: i.writable,
    warnings: i.warnings ?? [],
  };
}
