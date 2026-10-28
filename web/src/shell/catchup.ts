import type { Change, Overview } from './types';

export interface Visit { at: number; cursors: Record<string, number>; coveredAt?: Record<string, number> }
export interface Recap { items: { workspace: string; name: string; change: Change }[]; unavailable: string[]; incomplete: boolean }

/** Only current authorised projections participate; persisted state holds cursors, never Team content. */
export function recap(overview: Overview, previous: Visit, through: number): Recap {
  const out: Recap = { items: [], unavailable: [], incomplete: false };
  const seen = new Set<string>();
  const origins = new Set(overview.entries.filter(e => !e.error && e.workspace.state === 'ready').flatMap(e => (e.summary?.changes?.items ?? []).filter(c => Date.parse(c.at) > (previous.coveredAt?.[e.workspace.id] ?? previous.at) && Date.parse(c.at) <= through).map(c => c.key)));
  for (const e of overview.entries) {
    const data = e.summary?.changes;
    if (e.error || !data || e.workspace.state !== 'ready') { out.unavailable.push(e.workspace.name); continue; }
    const since = previous.coveredAt?.[e.workspace.id] ?? previous.at;
    if (data.incomplete || Date.parse(data.since) > since) out.incomplete = true;
    for (const c of data.items) {
      const at = Date.parse(c.at);
      if (at <= since || at > through || seen.has(c.key)) continue;
      // Integration source refs are canonical workspace/project/ticket/member identifiers.
      const source = c.origin?.match(/^werkbord-source:v1:([^:]+):([^:]+):([^:]+):/);
      if (source && origins.has(`ticket:${source[1]}:${source[2]}:${source[3]}`)) continue;
      seen.add(c.key); out.items.push({ workspace: e.workspace.id, name: e.workspace.name, change: c });
    }
  }
  out.items.sort((a, b) => Date.parse(b.change.at) - Date.parse(a.change.at));
  return out;
}

export const VISIT_KEY = 'werkbord.catch-up.visit.v1';
export function readVisit(storage: Pick<Storage, 'getItem'>): Visit | null {
  try {
    const v = JSON.parse(storage.getItem(VISIT_KEY) ?? 'null') as Visit | null;
    if (!v || !Number.isFinite(v.at) || v.at <= 0 || v.at > Date.now() || !v.cursors || typeof v.cursors !== 'object') return null;
    if (Object.entries(v.cursors).some(([key, value]) => key.length > 100 || !Number.isSafeInteger(value) || value < 0)) return null;
    if (v.coveredAt && (typeof v.coveredAt !== 'object' || Object.values(v.coveredAt).some(at => !Number.isFinite(at) || at <= 0 || at > Date.now()))) return null;
    return v;
  } catch { return null; }
}
