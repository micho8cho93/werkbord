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
    for (const tab of ['settings', 'labels', 'devices', 'hosts', 'connectivity', 'backups', 'license']) expect(activeKey('team', `?tab=${tab}`)).toBe('');
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
