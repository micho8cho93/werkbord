// What needs the person, across every workspace they can open. Pure: the app hands the shell each workspace's summary and
// these functions put them side by side. Nothing here asks a workspace anything, so a workspace that does not answer is a
// row that says so, never a reason another workspace's work is missing.

import { formatDateTime } from '../lib/clock.svelte';
import type { Attention, Entry, Execution, Overview, OverviewEntry, Scheduled, Severity, WorkItem } from './types';

export interface Labeled<T> {
  workspace: Entry;
  item: T;
}

const rank: Record<Severity, number> = { critical: 0, warning: 1, info: 2 };

const ok = (e: OverviewEntry): boolean => !!e.summary;

/** The person's own work, grouped by workspace in the order the switcher lists them. Workspaces with none are left out. */
export function myWork(o: Overview): { workspace: Entry; items: WorkItem[]; error?: string; snapshotAt?: string }[] {
  return o.entries
    .map((e) => ({ workspace: e.workspace, items: e.summary?.work ?? [], error: e.error, snapshotAt: e.summary?.at }))
    .filter((g) => g.items.length > 0 || g.error);
}

/** Only reports of running work; queued and starting work are separate. */
export function running(o: Overview): number {
  return executionCounts(o).running;
}

export function executionCounts(o: Overview): { running: number; queued: number; starting: number; unknown: number } {
  const counts = { running: 0, queued: 0, starting: 0, unknown: 0 };
  for (const e of o.entries) {
    for (const w of e.summary?.work ?? []) {
      if (!['running', 'queued', 'starting'].includes(w.execution ?? '')) continue;
      if (e.error || e.workspace.state !== 'ready' || w.report?.stale) counts.unknown++;
      else counts[w.execution as 'running' | 'queued' | 'starting']++;
    }
  }
  return counts;
}

const operational = (a: Attention): boolean => a.kind === 'host' || a.kind === 'runner';
const actionKinds = new Set(['needs_input', 'idle', 'blocked', 'failed', 'review', 'changes_requested', 'conflict', 'repository']);
export const actionable = (a: Attention): boolean => !operational(a) && (actionKinds.has(a.kind) || a.severity !== 'info');

/** Critical problems stay first; routine host advice never displaces an answer or review. */
export function attentionGroups(o: Overview): { id: string; label: string; rows: Labeled<Attention>[] }[] {
  const rows = attention(o);
  return [
    { id: 'urgent', label: 'Urgent', rows: rows.filter(r => r.item.severity === 'critical') },
    { id: 'actions', label: 'Action needed', rows: rows.filter(r => r.item.severity !== 'critical' && actionable(r.item)) },
    { id: 'warnings', label: 'Operational warnings', rows: rows.filter(r => r.item.severity !== 'critical' && !actionable(r.item)) },
  ].filter(g => g.rows.length > 0);
}

/** How much waits in each kind of workspace, so the two can be told apart at a glance: actions for the person, and operational warnings. */
export function tallyByKind(groups: ReturnType<typeof attentionGroups>): { kind: 'personal' | 'team'; label: string; actions: number; warnings: number }[] {
  return (['personal', 'team'] as const).map((kind) => {
    const rows = groups.flatMap((g) => g.rows.map((r) => ({ r, id: g.id }))).filter((x) => x.r.workspace.kind === kind);
    return {
      kind,
      label: kind === 'personal' ? 'Individual' : 'Team',
      actions: rows.filter((x) => x.id !== 'warnings').length,
      warnings: rows.filter((x) => x.id === 'warnings').length,
    };
  });
}

export function attentionAction(a: Attention): string {
  return ({ needs_input: 'Answer', idle: 'Review turn', blocked: 'Resolve blocker', failed: 'Inspect failure', review: 'Review changes', changes_requested: 'Review feedback', conflict: 'Inspect conflict', repository: 'Review changes', host: 'Inspect warning', runner: 'Inspect runner' } as Record<string, string>)[a.kind] ?? 'Open details';
}

export function executionLabel(execution: Execution): string {
  return ({ queued: 'Queued', starting: 'Starting', running: 'Running', needs_input: 'Waiting for an answer', idle: 'Waiting for your next message', waiting: 'Waiting', blocked: 'Blocked', completed: 'Session finished', failed: 'Failed', canceled: 'Stopped', '': '' })[execution];
}

export function recordedTime(at?: string): string {
  return at && Number.isFinite(Date.parse(at)) ? formatDateTime(at) : '';
}

/** Everything that waits for the person, worst first, then newest. */
export function attention(o: Overview): Labeled<Attention>[] {
  const rows: Labeled<Attention>[] = [];
  for (const e of o.entries) for (const a of e.summary?.attention ?? []) rows.push({ workspace: e.workspace, item: a });
  return rows.sort((a, b) => rank[a.item.severity] - rank[b.item.severity] || (b.item.at ?? '').localeCompare(a.item.at ?? ''));
}

export interface Counts {
  /** Actions waiting for the person, plus operational warnings. */
  needsYou: number;
  critical: number;
  /** Workspaces that did not answer. */
  unreachable: number;
}

export function counts(o: Overview): Counts {
  const rows = attention(o);
  return {
    needsYou: rows.filter((r) => actionable(r.item) || r.item.severity !== 'info').length,
    critical: rows.filter((r) => r.item.severity === 'critical').length,
    unreachable: o.entries.filter((e) => e.error || e.workspace.state === 'offline' || e.workspace.state === 'unavailable' || (!ok(e) && e.workspace.state === 'ready')).length,
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
