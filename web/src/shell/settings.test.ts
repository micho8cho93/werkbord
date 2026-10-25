import { describe, expect, it } from 'vitest';
import { general, groupsFor, pick, sectionForPlace, sectionsOf, teamsOf } from './settings';
import { isPlace } from './frames';
import type { Item } from './types';

const team = (id: string, name: string, state: Item['state'] = 'ready'): Item => ({ id, kind: 'team', name, state, source: 'x' });
const items: Item[] = [{ id: 'personal', kind: 'personal', name: 'Individual', state: 'ready', source: 'x' }, team('team:a', 'Acme'), team('team:b', 'Beta')];
const labels = (scope: Parameters<typeof groupsFor>[0]) => sectionsOf(groupsFor(scope, items)).map((s) => s.label);

describe('what Settings lists', () => {
  it('has the same groups in every scope, with only what applies in each', () => {
    const personal = groupsFor({ kind: 'personal' }, items).map((g) => g.label);
    const acme = groupsFor({ kind: 'team', id: 'team:a' }, items).map((g) => g.label);
    expect(personal).toEqual(['General', 'Work', 'Machines', 'Connections']);
    expect(acme).toEqual(['General', 'Work', 'Machines', 'Connections', 'Workspace']);
    expect(labels({ kind: 'personal' })).toContain('Task defaults');
    expect(labels({ kind: 'personal' })).toContain('Personal memory');
    expect(labels({ kind: 'personal' })).not.toContain('Labels');
    expect(labels({ kind: 'team', id: 'team:a' })).toContain('Team memory');
    expect(labels({ kind: 'team', id: 'team:a' })).toContain('Workspace Hosts');
    expect(labels({ kind: 'team', id: 'team:a' })).not.toContain('Task defaults');
    expect(labels({ kind: 'team', id: 'team:a' })).not.toContain('Personal memory');
  });

  it('has Plan among the window’s own settings in every scope: Free or Premium is about this person, not a workspace', () => {
    for (const scope of [{ kind: 'personal' }, { kind: 'universal' }, { kind: 'team', id: 'team:a' }] as const) {
      expect(sectionsOf(groupsFor(scope, items)).filter((s) => s.label === 'Plan')).toEqual([{ id: 'general/plan', label: 'Plan', where: { native: 'plan' } }]);
    }
  });

  it('always has the window’s own settings first, so Appearance and Alerts are the same everywhere', () => {
    for (const scope of [{ kind: 'personal' }, { kind: 'universal' }, { kind: 'team', id: 'team:b' }] as const) {
      expect(groupsFor(scope, items)[0]).toEqual(general());
    }
  });

  it('lists everything on Home, a workspace at a time, with no section twice', () => {
    const groups = groupsFor({ kind: 'universal' }, items);
    expect(groups.map((g) => g.owner).filter(Boolean).filter((o, i, a) => a.indexOf(o) === i)).toEqual(['Personal', 'Acme', 'Beta']);
    const ids = sectionsOf(groups).map((s) => s.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids).toContain('team:a/hosts');
    expect(ids).toContain('team:b/hosts');
    expect(ids).toContain('personal/phones');
  });

  it('leaves out a Team that cannot be shown, and a scope it does not know falls back to the first section', () => {
    expect(teamsOf([team('team:x', 'Gone', 'leaving'), team('team:y', 'Setting up', 'setup'), team('team:z', 'Ok')]).map((t) => t.id)).toEqual(['team:z']);
    const groups = groupsFor({ kind: 'personal' }, items);
    expect(pick(groups, 'team:a/hosts').id).toBe('general/appearance');
    expect(pick(groups, 'personal/phones').id).toBe('personal/phones');
  });

  it('only ever asks a workspace for a place inside itself', () => {
    for (const s of sectionsOf(groupsFor({ kind: 'universal' }, items))) if ('workspace' in s.where) expect(isPlace(s.where.place)).toBe(true);
  });
});

describe('which section a page’s link to settings means', () => {
  const groups = groupsFor({ kind: 'universal' }, items);
  it('follows Individual’s addresses', () => {
    expect(sectionForPlace(groups, 'personal', '#/settings?runners')?.id).toBe('personal/runners');
    expect(sectionForPlace(groups, 'personal', '#/settings?phones')?.id).toBe('personal/phones');
    expect(sectionForPlace(groups, 'personal', '#/settings')?.id).toBe('personal/defaults');
    expect(sectionForPlace(groups, 'personal', '#/settings?nothing')?.id).toBe('personal/defaults');
  });
  it('follows Team’s queries, including the parts of its settings page', () => {
    expect(sectionForPlace(groups, 'team:a', '?tab=devices')?.id).toBe('team:a/devices');
    expect(sectionForPlace(groups, 'team:b', '?tab=settings&part=phones')?.id).toBe('team:b/phones');
    expect(sectionForPlace(groups, 'team:a', '?tab=settings')?.id).toBe('team:a/computer');
    expect(sectionForPlace(groups, 'team:a', '?tab=backups')?.id).toBe('team:a/backups');
  });
});
