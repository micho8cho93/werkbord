import { describe, expect, it } from 'vitest';
import { entries, listed } from './nav';
import { isPlace } from './frames';

describe('workspace detail routes', () => {
  it('only ever ask a workspace for a place inside itself', () => {
    for (const kind of ['personal', 'team'] as const) {
      for (const e of entries(kind)) expect(isPlace(e.place)).toBe(true);
    }
    expect(entries('team').some(e => e.key === 'analytics' && e.place === '?tab=analytics')).toBe(true);
  });
  it('lists each project once, in the order the workspace gave', () => {
    const a = { id: 'a', name: 'A', href: '#/p/a/work' };
    const b = { id: 'b', name: 'B', href: '#/p/b/work' };
    expect(listed([b, a, { ...b, name: 'again' }]).map((p) => p.id)).toEqual(['b', 'a']);
    expect(listed(undefined)).toEqual([]);
  });
});

import { destinations, matching, sectionColour, sectionItems, subAreas } from './nav';
import type { Item, Overview } from './types';

const item = (id: string, kind: Item['kind'], name: string, state: Item['state'] = 'ready'): Item => ({ id, kind, name, state, source: 'x' });
const overview = (entries: Overview['entries']): Overview => ({ entries, at: '' });

describe('the sections of the sidebar', () => {
  it('lists Personal first and then each team, leaving out one that is only being set up or was left', () => {
    const items = [item('team:a', 'team', 'Acme'), item('team:s', 'team', 'Setting up', 'setup'), item('personal', 'personal', 'Individual'), item('team:b', 'team', 'Beta'), item('team:x', 'team', 'Left', 'leaving')];
    expect(sectionItems(items).map((i) => i.id)).toEqual(['personal', 'team:a', 'team:b']);
  });

  it('lists no sub-area in the sidebar: Projects and People are tabs inside the workspace, and only the palette names them', () => {
    expect(subAreas('personal').map((e) => e.key)).toEqual(['projects']);
    expect(subAreas('team').map((e) => e.key)).toEqual(['projects', 'members']);
    expect(subAreas('team').find((e) => e.key === 'members')?.title).toBe('People');
  });

  it('colours Personal blue and a team the same way every time', () => {
    expect(sectionColour('personal', 'personal')).toBe('var(--accent)');
    expect(sectionColour('team', 'team:a')).toBe(sectionColour('team', 'team:a'));
    expect(sectionColour('team', 'team:a')).toMatch(/^#[0-9A-F]{6}$/);
  });
});

describe('the go-to palette', () => {
  const items = [item('personal', 'personal', 'Individual'), item('team:main', 'team', 'Acme')];
  const summary = (id: string, projects: { id: string; name: string; href: string }[]) => ({ workspace: { id, kind: 'team' as const, name: id, state: 'ready' as const }, summary: { schema: 's', workspace: { id, kind: 'team' as const, name: id, state: 'ready' as const }, projects, work: [], attention: [], schedule: [], at: '' } });
  const o = overview([summary('personal', [{ id: 'p1', name: 'side-project', href: '#/p/p1' }]), summary('team:main', [{ id: 'w', name: 'web-app', href: '?tab=board&project=w' }])]);
  const all = destinations(items, o);

  it('names Personal, never Individual, and reaches every project and place', () => {
    const titles = all.map((d) => d.title);
    expect(titles).toContain('Personal');
    expect(titles).not.toContain('Individual');
    expect(all.find((d) => d.id === 'project:team:main:w')).toMatchObject({ title: 'web-app', hint: 'Acme · project', go: { workspace: 'team:main', place: '?tab=board&project=w' } });
    expect(all.find((d) => d.id === 'project:personal:p1')?.hint).toBe('Personal · project');
    expect(all.find((d) => d.id === 'place:team:main:members')?.go).toEqual({ workspace: 'team:main', place: '?tab=members' });
    expect(all.find((d) => d.id === 'page:reviews')?.go).toEqual({ page: 'reviews' });
    expect(all.find((d) => d.id === 'page:analytics')?.go).toEqual({ page: 'analytics' });
    expect(all.some((d) => d.id.startsWith('place:') && /reviews|analytics/.test(d.id))).toBe(false);
    expect(all.at(-1)).toMatchObject({ id: 'add-team', go: { addTeam: true } });
  });

  it('matches every word typed, titles first', () => {
    expect(matching(all, '').length).toBe(all.length);
    expect(matching(all, 'web').map((d) => d.title)).toEqual(['web-app']);
    expect(matching(all, 'acme people').map((d) => d.id)).toEqual(['place:team:main:members']);
    expect(matching(all, 'analy')[0].id).toBe('page:analytics');
    expect(matching(all, 'zzz')).toEqual([]);
    // "acme" names the team's own entry before its projects, which only match by their hint.
    expect(matching(all, 'acme')[0].id).toBe('ws:team:main');
  });
});

import { agentsPlace, scopedAgentsPlace } from './nav';
describe('the place Agents is shown at', () => {
  it('names the session, the project context and the way back, and is a place the window accepts', () => {
    const place = agentsPlace('run:run_a', 'prj_a', [{ kind: 'work' }]);
    expect(place).toBe('#/sessions/run%3Arun_a?project=prj_a&from=work');
    expect(isPlace(place)).toBe(true);
    expect(agentsPlace('', 'prj_a')).toBe('#/sessions?project=prj_a');
    expect(agentsPlace()).toBe('#/sessions');
  });
  it('is re-scoped to another project, or none, with the session and the way back kept', () => {
    const place = agentsPlace('run:run_a', 'prj_a', [{ kind: 'work' }]);
    expect(scopedAgentsPlace(place, 'prj_b')).toBe('#/sessions/run%3Arun_a?project=prj_b&from=work');
    expect(scopedAgentsPlace(place, '')).toBe('#/sessions/run%3Arun_a?from=work');
    expect(scopedAgentsPlace('#/sessions', '')).toBe('#/sessions');
  });
});
