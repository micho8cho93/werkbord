import { describe, expect, it } from 'vitest';
import { phoneSubtitle } from './subtitle';
import type { AgentCard } from './agents';

const card = (state: AgentCard['state']) => ({ id: state, name: state, state, sessions: [], guidance: '' }) as AgentCard;
const ctx = { needsYou: 0, review: 0, cards: [] as AgentCard[], runners: [] as { online: boolean; disabled: boolean }[] };

describe('phoneSubtitle', () => {
  it('counts what needs you in the title’s badge, and says nothing for zero', () => {
    expect(phoneSubtitle('control', { ...ctx, needsYou: 4 })).toMatchObject({ badge: 4 });
    expect(phoneSubtitle('control', ctx)!.badge).toBeUndefined();
  });

  it('says how the agents and runners stand', () => {
    expect(phoneSubtitle('agents', { ...ctx, cards: [card('Working'), card('Idle')] })).toEqual({ text: '1 running · 1 idle', dot: 'ok' });
    expect(phoneSubtitle('agents', { ...ctx, cards: [card('Waiting for you')] })).toMatchObject({ dot: 'ask' });
    expect(phoneSubtitle('runners', { ...ctx, runners: [{ online: true, disabled: false }, { online: false, disabled: false }, { online: true, disabled: true }] })).toEqual({ text: '1 online · 2 offline', dot: 'ok' });
    expect(phoneSubtitle('runners', ctx)).toEqual({ text: 'No runners yet', dot: 'neutral' });
  });

  it('has a line for My Work and Reviews, and none elsewhere', () => {
    expect(phoneSubtitle('mywork', ctx)!.text).toMatch(/tasks/);
    expect(phoneSubtitle('reviews', { ...ctx, review: 2 })!.text).toBe('2 tasks are ready for you to review');
    expect(phoneSubtitle('projects', ctx)).toBeNull();
  });
});
