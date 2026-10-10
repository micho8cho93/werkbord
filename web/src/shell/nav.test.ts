import { describe, expect, it } from 'vitest';
import { activeKey, entries, listed, runnersPlace, settingsPlace } from './nav';
import { isPlace } from './frames';

describe('the sidebar entries', () => {
  it('only ever ask a workspace for a place inside itself', () => {
    for (const kind of ['personal', 'team'] as const) {
      for (const e of entries(kind)) expect(isPlace(e.place)).toBe(true);
      expect(isPlace(settingsPlace(kind))).toBe(true);
    }
    expect(isPlace(runnersPlace('personal') ?? '')).toBe(true);
    expect(runnersPlace('team')).toBeUndefined();
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
    expect(activeKey('personal', '#/settings?runners')).toBe('settings');
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
    for (const tab of ['settings', 'labels', 'devices', 'hosts', 'connectivity', 'backups', 'license']) expect(activeKey('team', `?tab=${tab}`)).toBe('settings');
    for (const tab of ['board', 'timeline', 'repository', 'activity', 'people']) expect(activeKey('team', `?tab=${tab}&project=tpj_1`)).toBe('project:tpj_1');
    expect(activeKey('team', '?tab=board&project=tpj_1&ticket=t1')).toBe('project:tpj_1');
    expect(activeKey('team', '?tab=board')).toBe('');
    expect(activeKey('team', '?tab=mywork')).toBe('');
    expect(activeKey('team', '#/control')).toBe('');
  });
});
