import { describe, expect, it } from 'vitest';
import { activeRunners, connectionWord, deckCounts, deckKeys } from './deck';
import type { Overview, Runner } from '../types';

const counts = { needsYou: 0, review: 0, running: 0, repoAttention: 0 };

describe('deckKeys', () => {
  it('preserves all nine original navigation destinations in the catalogue', () => {
    expect(deckKeys(counts).map((k) => k.label)).toEqual(['Workspace', 'Calendar', 'Projects', 'My Work', 'Agents', 'Analytics', 'Reviews', 'Git', 'Runners']);
  });

  it('points every key at a page of its own', () => {
    const hrefs = deckKeys(counts).map((k) => k.href);
    expect(new Set(hrefs).size).toBe(9);
    expect(hrefs.every((h) => h.startsWith('#/'))).toBe(true);
  });

  it('reuses icons the app already draws, one per key', () => {
    expect(deckKeys(counts).map((k) => k.icon)).toEqual(['control', 'calendar', 'folder', 'check', 'activity', 'analytics', 'merge', 'git', 'runner']);
  });

  it('counts only what a key means, and says it to a screen reader', () => {
    const keys = Object.fromEntries(deckKeys({ needsYou: 3, review: 2, running: 1, repoAttention: 4 }).map((k) => [k.id, k]));
    expect(keys.needs).toMatchObject({ count: 3, tone: 'wait', note: '3 items need attention' });
    expect(keys.reviews).toMatchObject({ count: 2, note: '2 ready for review' });
    expect(keys.agents).toMatchObject({ count: 1, tone: 'work', note: '1 running' });
    expect(keys.git).toMatchObject({ count: 4, tone: 'wait' });
    expect(keys.calendar.count).toBe(0);
    expect(keys.runners.count).toBe(0);
  });

  it('says nothing for a count of zero', () => {
    expect(deckKeys(counts).every((k) => k.count === 0 && k.note === '')).toBe(true);
  });

  it('uses singular and plural attention labels for screen readers', () => {
    const one = Object.fromEntries(deckKeys({ ...counts, needsYou: 1, repoAttention: 1 }).map(k => [k.id, k]));
    expect(one.needs.note).toBe('1 item needs attention');
    expect(one.git.note).toBe('1 Git item needs attention');
    const many = Object.fromEntries(deckKeys({ ...counts, needsYou: 2, repoAttention: 2 }).map(k => [k.id, k]));
    expect(many.needs.note).toBe('2 items need attention');
    expect(many.git.note).toBe('2 Git items need attention');
  });
});

describe('deckCounts', () => {
  it('adds up every project and takes needs you from the app', () => {
    const overview = {
      projects: [
        { projectId: 'a', name: 'A', needsInput: 1, blocked: 0, idle: 0, running: 2, failed: 0, review: 1, repoAttention: 1, repoRisk: 0 },
        { projectId: 'b', name: 'B', needsInput: 0, blocked: 0, idle: 0, running: 1, failed: 0, review: 2, repoAttention: 0, repoRisk: 0 },
      ],
    } as unknown as Overview;
    expect(deckCounts(overview, 5)).toEqual({ needsYou: 5, review: 3, running: 3, repoAttention: 1 });
  });

  it('is all zeros until the overview arrives', () => {
    expect(deckCounts(null, 0)).toEqual(counts);
  });
});

describe('activeRunners', () => {
  it('counts the ones that are connected and switched on', () => {
    const r = (online: boolean, disabled: boolean) => ({ online, disabled }) as Runner;
    expect(activeRunners([r(true, false), r(true, true), r(false, false)])).toHaveLength(1);
  });
});

describe('connectionWord', () => {
  it('names the connection and its tone', () => {
    expect(connectionWord('live')).toEqual({ word: 'Connected', tone: 'ok' });
    expect(connectionWord('syncing')).toEqual({ word: 'Syncing', tone: 'warn' });
    expect(connectionWord('connecting')).toEqual({ word: 'Connecting', tone: 'warn' });
    expect(connectionWord('offline')).toEqual({ word: 'Offline', tone: 'bad' });
    expect(connectionWord('unauthorized').tone).toBe('bad');
  });
});
