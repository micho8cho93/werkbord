import { describe, expect, it } from 'vitest';
import { contextKey, parseContext, projectInHref, projectOptions, scopedOverview } from './project-context';
import type { Item, Overview, Summary } from './types';
const workspace = (id: string, kind: Item['kind'] = 'personal', state: Item['state'] = 'ready'): Item => ({ id, kind, state, name: id, source: 'fixture' });
const summary = (w: Item, ids: string[]): Summary => ({ schema: 'workspace-summary/v1', workspace: w, at: '', projects: ids.map(id => ({ id, name: 'Same name', href: w.kind === 'personal' ? `#/p/${id}/board` : `?tab=board&project=${id}` })), work: ids.map(id => ({ id: `task-${id}`, title: id, status: 'doing', execution: 'running', project: 'Same name', href: w.kind === 'personal' ? `#/p/${id}/task/x` : `?tab=board&project=${id}&ticket=x` })), attention: ids.map(id => ({ id, kind: 'review', severity: 'info', title: id, href: w.kind === 'personal' ? `#/p/${id}/git` : `?tab=board&project=${id}` })), schedule: [], planned: [] });
const overview = (items: Item[], ids = ['a', 'b']): Overview => ({ at: '', entries: items.map(w => ({ workspace: w, summary: summary(w, ids) })) });
describe('shared project context', () => {
  it.each([[workspace('personal')], [workspace('team:one', 'team'), workspace('team:two', 'team')], [workspace('personal'), workspace('team:one', 'team')]])('uses composite identity across personal and team projects: %j', (...items) => {
    const o = overview(items);
    expect(projectOptions(items, o)).toHaveLength(items.length * 2);
    const selected = { workspace: items.at(-1)!.id, project: 'b' };
    const result = scopedOverview(o, items, selected)!;
    expect(result.entries).toHaveLength(1);
    expect(result.entries[0].summary?.work.map(w => w.title)).toEqual(['b']);
    expect(result.entries[0].summary?.attention.map(w => w.title)).toEqual(['b']);
    expect(o.entries[0].summary?.work).toHaveLength(2);
  });
  it('supports one personal project and a virtual aggregate without synthesising projects', () => {
    const items = [workspace('personal')], o = overview(items, ['a']);
    expect(projectOptions(items, o)).toHaveLength(1);
    expect(scopedOverview(o, items, null)?.entries[0].summary?.projects.map(p => p.id)).toEqual(['a']);
  });
  it('aggregates only currently accessible workspaces and preserves explicit offline failures', () => {
    const items = [workspace('personal'), workspace('team:offline', 'team', 'offline')];
    const o = overview([...items, workspace('team:revoked', 'team')]);
    const result = scopedOverview(o, items, null)!;
    expect(result.entries).toHaveLength(2);
    expect(result.entries[1].summary).toBeUndefined();
    expect(result.entries[1].error).toContain('Reconnect');
    expect(scopedOverview(o, items, { workspace: 'team:offline', project: 'a' })?.entries[0].summary).toBeUndefined();
  });
  it('never recovers missing or restricted project content by name or another workspace', () => {
    const items = [workspace('personal'), workspace('team:restricted', 'team')], o = overview(items);
    o.entries[1].summary!.projects = o.entries[1].summary!.projects.filter(p => p.id === 'a');
    o.entries[1].summary!.work = o.entries[1].summary!.work.filter(w => w.title === 'a');
    expect(projectOptions(items, o).filter(p => p.workspace === 'team:restricted').map(p => p.project)).toEqual(['a']);
    expect(scopedOverview(o, items, { workspace: 'team:restricted', project: 'b' })?.entries[0].summary?.work).toEqual([]);
  });
  it('parses safe identities and route identifiers without delimiter collisions', () => {
    const p = { workspace: 'team:a|b', project: 'c|d' };
    expect(parseContext(contextKey(p))).toEqual(p);
    for (const invalid of ['', '{}', '["a"]', '["a", 2]', 'null']) expect(parseContext(invalid)).toBeNull();
    expect(projectInHref('personal', '#/p/a%20b/task/x')).toBe('a b');
    expect(projectInHref('personal', '#/p/%zz/task/x')).toBeNull();
    expect(projectInHref('team', 'https://external/?project=a')).toBeNull();
  });
});
