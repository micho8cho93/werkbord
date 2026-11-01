// What the sidebar says about the places it leads to: how many things in each are waiting for the person, and the worst kind of wait,
// so that a glance at the rail says whether something needs them and what sort. Pure, so it can be tested on its own.

import { reviewCount } from './reviews';
import type { Overview } from './types';

/** `ask` is an agent waiting to be told something, `bad` a failure or a conflict, `review` work to look over. Worst first. */
export type RailTone = 'ask' | 'bad' | 'review';

export interface RailBadge {
  count: number;
  tone: RailTone;
  /** Said in words for a screen reader and a tooltip, because colour alone is not enough. */
  label: string;
}

const noun = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

function badge(ask: number, bad: number, review: number): RailBadge | undefined {
  const count = ask + bad + review;
  if (!count) return undefined;
  const parts = [ask && `${noun(ask, 'question', 'questions')} waiting for an answer`, bad && `${noun(bad, 'failure', 'failures')}`, review && `${noun(review, 'review', 'reviews')}`].filter(Boolean);
  return { count, tone: ask ? 'ask' : bad ? 'bad' : 'review', label: parts.join(', ') };
}

/** The badges for the places that have anything to say. A place with nothing waiting has none: a rail of zeros is noise. */
export function railBadges(overview: Overview | null): { inbox?: RailBadge; agents?: RailBadge } {
  if (!overview) return {};
  let ask = 0;
  let bad = 0;
  for (const e of overview.entries) {
    if (!e.summary || e.error) continue;
    for (const a of e.summary.attention) {
      if (a.kind === 'needs_input') ask++;
      else if (a.kind === 'failed' || a.kind === 'conflict') bad++;
    }
  }
  return { inbox: badge(ask, bad, reviewCount(overview)), agents: badge(ask, bad, 0) };
}
