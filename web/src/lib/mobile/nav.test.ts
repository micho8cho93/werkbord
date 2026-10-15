import { describe, expect, it } from 'vitest';
import { advance, parentHref, parentTitle, phoneParent, titleOf } from './nav';

describe('phoneParent', () => {
  it('puts the keys’ pages under the deck and the deck under nothing', () => {
    for (const view of ['control', 'projects', 'analytics', 'mywork', 'schedule', 'agents', 'reviews', 'repositories', 'runners', 'settings'] as const) {
      expect(phoneParent({ view, projectId: '', taskId: '' })).toEqual({ view: 'deck', projectId: '', taskId: '' });
    }
    expect(phoneParent({ view: 'deck', projectId: '', taskId: '' })).toBeNull();
  });

  it('puts a project’s pages under the list of projects, and a task on its board', () => {
    expect(phoneParent({ view: 'git', projectId: 'p', taskId: '' })).toEqual({ view: 'projects', projectId: '', taskId: '' });
    expect(phoneParent({ view: 'runs', projectId: 'p', taskId: '' })).toEqual({ view: 'projects', projectId: '', taskId: '' });
    expect(phoneParent({ view: 'task', projectId: 'p', taskId: 't' })).toEqual({ view: 'board', projectId: 'p', taskId: '' });
  });

  it('puts Git’s drill-down on Git', () => {
    expect(phoneParent({ view: 'git', projectId: 'p', taskId: '', sub: 'branches/x' })).toEqual({ view: 'git', projectId: 'p', taskId: '' });
  });
});

describe('parentHref and parentTitle', () => {
  it('gives an address and the words for it', () => {
    expect(parentHref({ view: 'mywork', projectId: '', taskId: '' })).toBe('#/deck');
    expect(parentTitle({ view: 'mywork', projectId: '', taskId: '' })).toBe('Control Deck');
    expect(parentHref({ view: 'task', projectId: 'p', taskId: 't' })).toBe('#/p/p/board');
    expect(parentTitle({ view: 'task', projectId: 'p', taskId: 't' })).toBe('Board');
    expect(parentTitle({ view: 'board', projectId: 'p', taskId: '' })).toBe('Projects');
    expect(parentHref({ view: 'deck', projectId: '', taskId: '' })).toBe('#/deck');
  });
});

describe('titleOf', () => {
  it('names the pages the phone has', () => {
    expect(titleOf('repositories')).toBe('Git');
    expect(titleOf('schedule')).toBe('Calendar');
    expect(titleOf('board')).toBe('');
  });
});

describe('advance', () => {
  it('goes on, and goes back when the page before is the one it arrives at', () => {
    let t = ['#/deck'];
    t = advance(t, '#/mywork');
    t = advance(t, '#/p/a/task/b');
    expect(t).toEqual(['#/deck', '#/mywork', '#/p/a/task/b']);
    t = advance(t, '#/mywork');
    expect(t).toEqual(['#/deck', '#/mywork']);
  });

  it('keeps one entry for the same page twice', () => {
    expect(advance(['#/deck'], '#/deck')).toEqual(['#/deck']);
  });

  it('replaces the current entry when an address is settled', () => {
    expect(advance(['#/deck', '#/p//board'], '#/p/a/board', true)).toEqual(['#/deck', '#/p/a/board']);
  });

  it('is bounded', () => {
    let t = ['#/0'];
    for (let i = 1; i < 60; i++) t = advance(t, `#/${i}`);
    expect(t.length).toBeLessThanOrEqual(30);
  });
});
