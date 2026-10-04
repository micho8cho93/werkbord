import { describe, expect, it } from 'vitest';
import { DEFAULT_POLICY, INTERACTION_OPTIONS, blockerLine, interactionLabel, interactionShort, isInteraction, isNotable } from './policy';
import type { Run } from './types';

describe('the interaction choices', () => {
  it('are the three the user is offered, in order, with "Ask me when needed" first', () => {
    expect(INTERACTION_OPTIONS.map((o) => o.label)).toEqual(['Ask me when needed', 'Work autonomously', 'Work autonomously — stop if blocked']);
    expect(INTERACTION_OPTIONS.map((o) => o.value)).toEqual(['interactive', 'autonomous', 'autonomous_stop_if_blocked']);
  });

  it('default to asking', () => {
    expect(DEFAULT_POLICY).toEqual({ interaction: 'interactive' });
    expect(INTERACTION_OPTIONS[0].value).toBe(DEFAULT_POLICY.interaction);
    expect(interactionLabel(undefined)).toBe('Ask me when needed');
    expect(interactionShort({ interaction: 'autonomous_stop_if_blocked' })).toBe('Autonomous, stops if blocked');
  });

  it('only accept values the controller knows', () => {
    expect(isInteraction('autonomous')).toBe(true);
    expect(isInteraction('yolo')).toBe(false);
    expect(isInteraction('')).toBe(false);
  });

  it('never suggest that autonomous means unrestricted', () => {
    for (const o of INTERACTION_OPTIONS) {
      expect(`${o.label} ${o.description}`.toLowerCase()).not.toMatch(/unrestricted|full access|bypass|without permission/);
    }
  });

  it('say what a card should mention: anything but the default', () => {
    expect(isNotable({ interaction: 'interactive' })).toBe(false);
    expect(isNotable(undefined)).toBe(false);
    expect(isNotable({ interaction: 'autonomous' })).toBe(true);
  });
});

describe('blockerLine', () => {
  it('is the summary, or a plain sentence if there is none', () => {
    expect(blockerLine({ blocker: { summary: 'Which region?', source: 'question', raisedAt: '' } } as Pick<Run, 'blocker'>)).toBe('Which region?');
    expect(blockerLine({})).toBe('The agent stopped rather than guess.');
  });
});
