// The line under a phone page's title, which says how the thing the page is about stands (the reference designs' "4 running · 2 idle",
// "3 online · 1 offline"). Only counts the controller reports, and only a line where there is something to say. No framework code.

import type { View } from '../location';
import { fleetLine, type AgentCard } from './agents';

export interface SubtitleContext {
  needsYou: number;
  review: number;
  cards: readonly AgentCard[];
  runners: readonly { online: boolean; disabled: boolean }[];
}

export interface Subtitle {
  text: string;
  /** A small dot before the words: whether things are well. */
  dot?: 'ok' | 'neutral' | 'ask';
  /** A count to put in the title's own badge. */
  badge?: number;
}

export function phoneSubtitle(view: View, c: SubtitleContext): Subtitle | null {
  switch (view) {
    case 'control':
      return { text: 'Items that need your attention', badge: c.needsYou || undefined };
    case 'mywork':
      return { text: 'Your tasks, plan and progress' };
    case 'reviews':
      return { text: c.review ? `${c.review} ${c.review === 1 ? 'task is' : 'tasks are'} ready for you to review` : 'Work ready for you to review' };
    case 'agents':
      return { text: fleetLine(c.cards), dot: c.cards.some((a) => a.state === 'Waiting for you') ? 'ask' : c.cards.some((a) => a.state === 'Working') ? 'ok' : 'neutral' };
    case 'runners': {
      const on = c.runners.filter((r) => r.online && !r.disabled).length;
      const off = c.runners.length - on;
      if (!c.runners.length) return { text: 'No runners yet', dot: 'neutral' };
      return { text: [`${on} online`, off ? `${off} offline` : ''].filter(Boolean).join(' · '), dot: on ? 'ok' : 'neutral' };
    }
    default:
      return null;
  }
}
