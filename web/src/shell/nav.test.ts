import { describe, expect, it } from 'vitest';
import { activeKey, entries, listed } from './nav';
import { isPlace } from './frames';

describe('the sidebar entries', () => {
  it('only ever ask a workspace for a place inside itself', () => {
    for (const kind of ['personal', 'team'] as const) {
      for (const e of entries(kind)) expect(isPlace(e.place)).toBe(true);
    }
    expect(entries('team').some(e => e.key === 'analytics' && e.place === '?tab=analytics')).toBe(true);
  });
  it('lists each project once, in the order the workspace gave', () => {
    const a = { id: 'a', name: 'A', href: '#/p/a/board' };
    const b = { id: 'b', name: 'B', href: '#/p/b/board' };
    expect(listed([b, a, { ...b, name: 'again' }]).map((p) => p.id)).toEqual(['b', 'a']);
    expect(listed(undefined)).toEqual([]);
  });
});

describe('which entry the current place belongs to', () => {
  it('follows Individual’s addresses', () => {
    expect(activeKey('personal', '#/control')).toBe('home');
    expect(activeKey('personal', '#/projects')).toBe('projects');
    // Settings is the window's own screen, not an entry of a workspace.
    expect(activeKey('personal', '#/settings?runners')).toBe('');
    expect(activeKey('personal', '#/p/prj_1/git/branch?x=1')).toBe('project:prj_1');
    expect(activeKey('personal', '#/p/prj_1/task/tsk_1')).toBe('project:prj_1');
    expect(activeKey('personal', '#/p/my%20repo/board')).toBe('project:my repo');
    expect(activeKey('personal', '#/')).toBe('');
    expect(activeKey('personal', undefined)).toBe('');
  });
  it('follows Team’s queries', () => {
    expect(activeKey('team', '?tab=workspace')).toBe('home');
    expect(activeKey('team', '?tab=projects')).toBe('projects');
    expect(activeKey('team', '?tab=reviews')).toBe('reviews');
    expect(activeKey('team', '?tab=members')).toBe('members');
    expect(activeKey('team', '?tab=analytics')).toBe('analytics');
    for (const tab of ['settings', 'labels', 'devices', 'hosts', 'connectivity', 'backups']) expect(activeKey('team', `?tab=${tab}`)).toBe('');
    for (const tab of ['board', 'timeline', 'repository', 'activity', 'project-settings', 'people']) expect(activeKey('team', `?tab=${tab}&project=tpj_1`)).toBe('project:tpj_1');
    expect(activeKey('team', '?tab=board&project=tpj_1&ticket=t1')).toBe('project:tpj_1');
    expect(activeKey('team', '?tab=board')).toBe('');
    expect(activeKey('team', '?tab=mywork')).toBe('');
    expect(activeKey('team', '#/control')).toBe('');
  });
});

import { generatedEntries, generatedWorkspaceID } from './nav';
import { todoWorkspace } from '../lib/personal/example';
import type { Workspace } from '../lib/personal/schema';
it('uses one stable personal ID across all assigned contexts, without duplicates or implicit Team access', () => {
  const workspace = {...todoWorkspace('pcl_abcdefghijklmnop'), id: 'pws_abcdefghijklmnop', revision: 1, createdAt: '', updatedAt: '', visibility: {universal: true, individual: true, teamWorkspaceIds: ['team:main']}} as Workspace;
  const all = [workspace, {...workspace}];
  expect(generatedEntries(all, true, 'personal')).toEqual([workspace]);
  expect(generatedEntries(all, false, 'personal')).toEqual([workspace]);
  expect(generatedEntries(all, false, 'team:main')).toEqual([workspace]);
  expect(generatedEntries(all, false, 'team:other')).toEqual([]);
});

it('opens approved workspace links, including revision reload hints, as shell pages', () => {
  const id = 'pws_abcdefghijklmnop';
  expect(generatedWorkspaceID(`#/personal?id=${id}`)).toBe(id);
  expect(generatedWorkspaceID(`#/personal?id=${id}&revision=3`)).toBe(id);
  for(const place of [`#/personal?id=${id}&revision=0`, `#/personal?id=${id}&url=evil`, '#/personal?id=../bad']) expect(generatedWorkspaceID(place)).toBeUndefined();
});

import { destinations, matching, sectionColour, sectionItems, sectionPlaces } from './nav';
import type { Item, Overview } from './types';

const item = (id: string, kind: Item['kind'], name: string, state: Item['state'] = 'ready'): Item => ({ id, kind, name, state, source: 'x' });
const overview = (entries: Overview['entries']): Overview => ({ entries, at: '' });

describe('the sections of the sidebar', () => {
  it('lists Personal first and then each team, leaving out one that is only being set up or was left', () => {
    const items = [item('team:a', 'team', 'Acme'), item('team:s', 'team', 'Setting up', 'setup'), item('personal', 'personal', 'Individual'), item('team:b', 'team', 'Beta'), item('team:x', 'team', 'Left', 'leaving')];
    expect(sectionItems(items).map((i) => i.id)).toEqual(['personal', 'team:a', 'team:b']);
  });

  it('gives each section the places after its projects, and no Home or Projects entry twice', () => {
    expect(sectionPlaces('personal').map((e) => e.key)).toEqual(['analytics']);
    expect(sectionPlaces('team').map((e) => e.key)).toEqual(['reviews', 'members', 'analytics']);
    expect(sectionPlaces('team').find((e) => e.key === 'members')?.title).toBe('People');
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
    expect(all.at(-1)).toMatchObject({ id: 'add-team', go: { addTeam: true } });
  });

  it('matches every word typed, titles first', () => {
    expect(matching(all, '').length).toBe(all.length);
    expect(matching(all, 'web').map((d) => d.title)).toEqual(['web-app']);
    expect(matching(all, 'acme people').map((d) => d.id)).toEqual(['place:team:main:members']);
    expect(matching(all, 'zzz')).toEqual([]);
    // "acme" names the team's own entry before its projects, which only match by their hint.
    expect(matching(all, 'acme')[0].id).toBe('ws:team:main');
  });
});
