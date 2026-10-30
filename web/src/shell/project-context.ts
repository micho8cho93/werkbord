// Presentation-only context. Workspace summaries remain the authority for accessible projects.
import type { Item, Overview, OverviewEntry } from './types';

export interface ProjectIdentity { workspace: string; project: string }
export interface ProjectOption extends ProjectIdentity { name: string; owner: string; kind: Item['kind'] }
export const contextKey = (p: ProjectIdentity | null): string => p ? JSON.stringify([p.workspace, p.project]) : '';
export function parseContext(value: string): ProjectIdentity | null {
  try {
    const pair: unknown = JSON.parse(value);
    return Array.isArray(pair) && pair.length === 2 && pair.every(v => typeof v === 'string' && v.length > 0 && v.length <= 200)
      ? { workspace: pair[0], project: pair[1] } : null;
  } catch { return null; }
}
export function projectOptions(items: readonly Item[], overview: Overview | null): ProjectOption[] {
  return items.filter(i => !['setup', 'leaving'].includes(i.state)).flatMap(i => {
    const seen = new Set<string>();
    return (overview?.entries.find(e => e.workspace.id === i.id)?.summary?.projects ?? [])
      .filter(p => !seen.has(p.id) && !!seen.add(p.id))
      .map(p => ({ workspace: i.id, project: p.id, name: p.name, owner: i.kind === 'personal' ? 'Personal' : i.name, kind: i.kind }));
  });
}
/** Extract only the workspace's own local route, never match by a display name. */
export function projectInHref(kind: Item['kind'], href = ''): string | null {
  if (kind === 'team') return href.startsWith('?') ? new URLSearchParams(href).get('project') : null;
  const match = /^#\/p\/([^/?#]+)(?:[/?#]|$)/.exec(href);
  try { return match ? decodeURIComponent(match[1]) : null; } catch { return null; }
}
export function scopedOverview(overview: Overview | null, items: readonly Item[], selected: ProjectIdentity | null): Overview | null {
  if (!overview) return null;
  const entries: OverviewEntry[] = items.filter(i => !['setup', 'leaving'].includes(i.state) && (!selected || i.id === selected.workspace)).map(workspace => {
    const entry = overview.entries.find(e => e.workspace.id === workspace.id);
    // Never use last-reported records to imply current access while a workspace is unavailable.
    if (workspace.state !== 'ready' || !entry?.summary || entry.error) return { workspace, error: entry?.error || workspace.detail || 'Workspace unavailable. Reconnect to refresh its projects.' };
    if (!selected) return { ...entry, workspace };
    const s = entry.summary;
    const belongs = (row: { href?: string }): boolean => projectInHref(workspace.kind, row.href) === selected.project;
    return { ...entry, workspace, summary: { ...s,
      queue: s.queue?.filter(belongs),
      projects: s.projects.filter(p => p.id === selected.project), work: s.work.filter(belongs),
      attention: s.attention.filter(belongs), schedule: s.schedule.filter(belongs), planned: s.planned?.filter(belongs),
      // Some events have no project route. Omit them and disclose incomplete coverage.
      changes: s.changes ? { ...s.changes, items: s.changes.items.filter(belongs),
        incomplete: s.changes.incomplete || s.changes.items.some(c => !projectInHref(workspace.kind, c.href)),
      } : undefined,
    } };
  });
  return { ...overview, entries };
}
