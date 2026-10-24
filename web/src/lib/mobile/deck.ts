// The navigation button catalogue and live counts. The saved nine-position layout and workflow catalogue live in layout.ts.

import type { IconName } from '../Icon.svelte';
import { globalHref } from '../location';
import type { Overview, Runner } from '../types';

export type DeckKeyId = 'needs' | 'calendar' | 'projects' | 'mywork' | 'agents' | 'analytics' | 'reviews' | 'git' | 'runners';

/** How a key's count is drawn: amber for what waits for the person, cobalt for work going on, none for a plain count. */
export type DeckTone = 'wait' | 'work' | 'plain';

export interface DeckKey {
  id: string;
  label: string;
  icon: IconName;
  workspaceIcon?: string;
  href: string;
  /** What the key counts, when it counts anything. Zero is not shown. */
  count: number;
  tone: DeckTone;
  /** What a screen reader hears after the label. */
  note: string;
}

export interface DeckCounts {
  needsYou: number;
  review: number;
  running: number;
  repoAttention: number;
}

const plural = (n: number, one: string, many = `${one}s`): string => `${n} ${n === 1 ? one : many}`;

/**
 * The navigation catalogue, in the original order. Each key's icon is the one the desktop navigation already uses for that
 * place (the sidebar's and rail's), so nothing here is a new picture for something the app already draws.
 */
export function deckKeys(c: DeckCounts): DeckKey[] {
  return [
    { id: 'needs', label: 'Workspace', icon: 'control', href: globalHref('control'), count: c.needsYou, tone: 'wait', note: c.needsYou ? `${plural(c.needsYou, 'item')} need you` : '' },
    { id: 'calendar', label: 'Calendar', icon: 'calendar', href: globalHref('schedule'), count: 0, tone: 'plain', note: '' },
    { id: 'projects', label: 'Projects', icon: 'folder', href: globalHref('projects'), count: 0, tone: 'plain', note: '' },
    { id: 'mywork', label: 'My Work', icon: 'check', href: globalHref('mywork'), count: 0, tone: 'plain', note: '' },
    { id: 'agents', label: 'Agents', icon: 'activity', href: globalHref('agents'), count: c.running, tone: 'work', note: c.running ? `${c.running} running` : '' },
    { id: 'analytics', label: 'Analytics', icon: 'analytics', href: globalHref('analytics'), count: 0, tone: 'plain', note: '' },
    { id: 'reviews', label: 'Reviews', icon: 'merge', href: globalHref('reviews'), count: c.review, tone: 'work', note: c.review ? `${c.review} ready for review` : '' },
    { id: 'git', label: 'Git', icon: 'git', href: globalHref('repositories'), count: c.repoAttention, tone: 'wait', note: c.repoAttention ? `${plural(c.repoAttention, 'Git item')} need attention` : '' },
    { id: 'runners', label: 'Runners', icon: 'runner', href: globalHref('runners'), count: 0, tone: 'plain', note: '' },
  ];
}

/** Where a generated personal workspace's button, and its row on the deck, goes. */
export const workspaceHref = (id: string): string => `#/personal?id=${id}`;

/** The counts the keys show, from what the controller reports about every project. */
export function deckCounts(overview: Overview | null, needsYou: number): DeckCounts {
  const projects = overview?.projects ?? [];
  const sum = (f: (p: Overview['projects'][number]) => number) => projects.reduce((n, p) => n + f(p), 0);
  return { needsYou, review: sum((p) => p.review), running: sum((p) => p.running), repoAttention: sum((p) => p.repoAttention) };
}

/** Runners the person's computer can use right now: connected and not switched off. */
export function activeRunners(runners: readonly Runner[]): Runner[] {
  return runners.filter((r) => r.online && !r.disabled);
}

/** The connection, in the word and tone the top bar uses for it. */
export function connectionWord(c: 'connecting' | 'syncing' | 'live' | 'offline' | 'unauthorized'): { word: string; tone: 'ok' | 'warn' | 'bad' } {
  if (c === 'live') return { word: 'Connected', tone: 'ok' };
  if (c === 'offline') return { word: 'Offline', tone: 'bad' };
  if (c === 'unauthorized') return { word: 'Signed out', tone: 'bad' };
  return { word: c === 'syncing' ? 'Syncing' : 'Connecting', tone: 'warn' };
}
