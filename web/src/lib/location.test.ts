import { describe, expect, it } from 'vitest';
import {
  GLOBAL_VIEWS,
  PROJECT_SECTIONS,
  globalHref,
  hrefOf,
  inProject,
  parse,
  projectHref,
  resolved,
  switchedTo,
  taskHref,
} from './location';

describe('parse', () => {
  it('knows the two global pages', () => {
    expect(parse('#/control')).toEqual({ view: 'control', projectId: '', taskId: '' });
    expect(parse('#/projects')).toEqual({ view: 'projects', projectId: '', taskId: '' });
  });

  it('knows a project and each of its sections, and defaults to its board', () => {
    expect(parse('#/p/prj_a')).toEqual({ view: 'board', projectId: 'prj_a', taskId: '' });
    expect(parse('#/p/prj_a/board')).toEqual({ view: 'board', projectId: 'prj_a', taskId: '' });
    expect(parse('#/p/prj_a/git')).toEqual({ view: 'git', projectId: 'prj_a', taskId: '' });
    expect(parse('#/p/prj_a/activity')).toEqual({ view: 'activity', projectId: 'prj_a', taskId: '' });
    expect(parse('#/p/prj_a/nonsense')).toEqual({ view: 'board', projectId: 'prj_a', taskId: '' });
  });

  it('knows a task, which is always in a project', () => {
    expect(parse('#/p/prj_a/task/tsk_1')).toEqual({ view: 'task', projectId: 'prj_a', taskId: 'tsk_1' });
    expect(parse('#/p/prj_a/task/tsk_1?x=1')).toEqual({ view: 'task', projectId: 'prj_a', taskId: 'tsk_1' });
    expect(parse('#/p/prj_a/task/')).toEqual({ view: 'board', projectId: 'prj_a', taskId: '' });
  });

  it('reads an older link that names no project as a page in the project last used', () => {
    expect(parse('#/board')).toEqual({ view: 'board', projectId: '', taskId: '' });
    expect(parse('#/git')).toEqual({ view: 'git', projectId: '', taskId: '' });
    expect(parse('#/activity')).toEqual({ view: 'activity', projectId: '', taskId: '' });
    expect(parse('')).toEqual({ view: 'board', projectId: '', taskId: '' });
    expect(parse('#/nonsense')).toEqual({ view: 'board', projectId: '', taskId: '' });
    expect(parse('#/p/')).toEqual({ view: 'board', projectId: '', taskId: '' });
  });

  it('survives a mangled id', () => {
    expect(parse('#/p/%E0%A4%A/board')).toEqual({ view: 'board', projectId: '', taskId: '' });
    expect(parse('#/p/prj_a/task/%E0%A4%A')).toEqual({ view: 'board', projectId: 'prj_a', taskId: '' });
  });
});

describe('links', () => {
  it('round-trip, including ids that need escaping', () => {
    expect(globalHref('control')).toBe('#/control');
    expect(projectHref('prj_a')).toBe('#/p/prj_a/board');
    expect(projectHref('prj_a', 'git')).toBe('#/p/prj_a/git');
    expect(taskHref('prj_a', 'tsk_1')).toBe('#/p/prj_a/task/tsk_1');
    expect(parse(taskHref('prj/a b', 'tsk/1 2'))).toEqual({ view: 'task', projectId: 'prj/a b', taskId: 'tsk/1 2' });
    for (const loc of [
      { view: 'control', projectId: '', taskId: '' },
      { view: 'projects', projectId: '', taskId: '' },
      { view: 'board', projectId: 'p', taskId: '' },
      { view: 'activity', projectId: 'p', taskId: '' },
      { view: 'task', projectId: 'p', taskId: 't' },
    ] as const) {
      expect(parse(hrefOf(loc))).toEqual(loc);
    }
  });

  it('say which pages are in a project', () => {
    expect(inProject('board')).toBe(true);
    expect(inProject('task')).toBe(true);
    expect(inProject('control')).toBe(false);
    expect(inProject('projects')).toBe(false);
  });
});

// Calendar will be a further section of a project. The navigation is built from this list, so adding one
// entry to it is all the shell, the switcher and the tab bar need.
describe('navigation is data, so it can grow', () => {
  it('lists the sections of a project and the global pages once', () => {
    expect(PROJECT_SECTIONS.map((s) => s.id)).toEqual(['board', 'git', 'activity']);
    expect(GLOBAL_VIEWS.map((g) => g.id)).toEqual(['control', 'projects']);
  });

  it('keeps any listed section when switching projects', () => {
    for (const s of PROJECT_SECTIONS) {
      expect(switchedTo({ view: s.id, projectId: 'prj_a', taskId: '' }, 'prj_b')).toEqual({ view: s.id, projectId: 'prj_b', taskId: '' });
    }
  });
});

describe('switching projects', () => {
  it('stays in the same section, in the new project', () => {
    expect(switchedTo({ view: 'git', projectId: 'prj_a', taskId: '' }, 'prj_b')).toEqual({ view: 'git', projectId: 'prj_b', taskId: '' });
    expect(switchedTo({ view: 'activity', projectId: 'prj_a', taskId: '' }, 'prj_b')).toEqual({ view: 'activity', projectId: 'prj_b', taskId: '' });
    expect(hrefOf(switchedTo(parse('#/p/prj_a/git'), 'prj_b'))).toBe('#/p/prj_b/git');
  });

  it('leaves a task behind: it belongs to the project it is in', () => {
    const to = switchedTo({ view: 'task', projectId: 'prj_a', taskId: 'tsk_1' }, 'prj_b');
    expect(to).toEqual({ view: 'board', projectId: 'prj_b', taskId: '' });
  });

  it('enters the new project at its board from a global page', () => {
    expect(switchedTo({ view: 'control', projectId: '', taskId: '' }, 'prj_b')).toEqual({ view: 'board', projectId: 'prj_b', taskId: '' });
    expect(switchedTo({ view: 'projects', projectId: '', taskId: '' }, 'prj_b')).toEqual({ view: 'board', projectId: 'prj_b', taskId: '' });
  });

  it('never carries a task or the old project into the new address', () => {
    for (const from of ['#/p/prj_a/task/tsk_1', '#/p/prj_a/board', '#/control', '#/p/prj_a/git']) {
      const href = hrefOf(switchedTo(parse(from), 'prj_b'));
      expect(href).toContain('prj_b');
      expect(href).not.toContain('prj_a');
      expect(href).not.toContain('tsk_1');
    }
  });
});

describe('resolved', () => {
  const ids = ['prj_a', 'prj_b'];

  it('leaves a page that names its project alone, and global pages too', () => {
    expect(resolved(parse('#/p/prj_b/git'), ids, 'prj_a')).toEqual(parse('#/p/prj_b/git'));
    expect(resolved(parse('#/control'), ids, 'prj_a')).toEqual(parse('#/control'));
  });

  it('puts a page that names none in the project last used', () => {
    expect(resolved(parse('#/git'), ids, 'prj_b')).toEqual({ view: 'git', projectId: 'prj_b', taskId: '' });
    expect(resolved(parse(''), ids, 'prj_b')).toEqual({ view: 'board', projectId: 'prj_b', taskId: '' });
  });

  it('falls back to the first project if the last used is gone, and to Projects if there are none', () => {
    expect(resolved(parse('#/board'), ids, 'prj_gone').projectId).toBe('prj_a');
    expect(resolved(parse('#/board'), ids, '').projectId).toBe('prj_a');
    expect(resolved(parse('#/board'), [], 'prj_a')).toEqual({ view: 'projects', projectId: '', taskId: '' });
  });
});
