import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Recommendation, RecommendationSet } from './types';

const server = vi.hoisted(() => ({
  set: null as unknown,
  fail: false,
  asked: [] as string[],
  feedback: [] as unknown[],
  feedbackFails: false,
  gate: null as Promise<void> | null,
}));

vi.mock('../api', () => ({
  ApiError: class extends Error {},
  api: {
    recommendations: async (project: string) => {
      server.asked.push(project);
      if (server.gate) await server.gate;
      if (server.fail) throw new Error('Suggestions are not enabled.');
      return server.set;
    },
    recommendationFeedback: async (project: string, body: unknown) => {
      server.feedback.push({ project, body });
      if (server.feedbackFails) throw new Error('memory is full');
      return { feedback: {} };
    },
  },
}));

import { recommendations } from './store.svelte';

const rec = (id: string, projectId = 'prj_1'): Recommendation => ({
  id, projectId, intent: 'find_bugs', variant: 'default', title: 'Scan for bugs', detail: 'x', basis: 'evidence', confidence: 'high',
  action: { kind: 'open_workflow', label: 'Show on Deck', workflow: 'find-bugs', startsExecution: false, confirmation: 'at_destination' }, evidence: [], applied: [],
});
const idOf = (n: number) => `rec_${n.toString(16).padStart(20, '0')}`;
const set = (items: Recommendation[], over: Partial<RecommendationSet> = {}): RecommendationSet =>
  ({ projectId: 'prj_1', generatedAt: '2026-10-19T09:00:00Z', items, standing: [], withheld: [], sources: {} as never, notes: [], boundary: 'x', feedbackStoredIn: 'memory', ...over });

beforeEach(() => {
  server.set = set([rec(idOf(1))]);
  Object.assign(server, { fail: false, asked: [], feedback: [], feedbackFails: false, gate: null });
  for (const k of Object.keys(recommendations.entries)) delete recommendations.entries[k];
  for (const k of Object.keys(recommendations.given)) delete recommendations.given[k];
  for (const k of Object.keys(recommendations.failed)) delete recommendations.failed[k];
});

describe('asking for suggestions', () => {
  it('asks once for several screens at the same moment, and not again while the answer is fresh', async () => {
    let open!: () => void;
    server.gate = new Promise((r) => (open = r));
    const all = Promise.all([recommendations.load('prj_1'), recommendations.load('prj_1'), recommendations.load('prj_1')]);
    open();
    await all;
    await recommendations.load('prj_1');
    expect(server.asked).toEqual(['prj_1']);
    expect(recommendations.shown('prj_1').map((r) => r.id)).toEqual([idOf(1)]);
    await recommendations.load('prj_1', true);
    expect(server.asked).toHaveLength(2);
  });

  it('keeps the last answer when the next ask fails, and says nothing loudly', async () => {
    await recommendations.load('prj_1');
    server.fail = true;
    await recommendations.load('prj_1', true);
    expect(recommendations.entry('prj_1')).toMatchObject({ status: 'error', error: 'Suggestions could not be loaded.' });
    expect(recommendations.shown('prj_1')).toHaveLength(1);
  });

  it('asks about nothing when there is no project', async () => {
    await recommendations.load('');
    expect(server.asked).toEqual([]);
    expect(recommendations.shown('')).toEqual([]);
  });
});

describe('a verdict on a suggestion', () => {
  it('is kept in memory where the controller serves it, and a dismissal takes the suggestion off the screen', async () => {
    await recommendations.load('prj_1');
    await recommendations.feedback(rec(idOf(1)), 'dismissed');
    expect(server.feedback).toEqual([{ project: 'prj_1', body: { recommendationId: idOf(1), intent: 'find_bugs', variant: 'default', verdict: 'dismissed' } }]);
    expect(recommendations.shown('prj_1')).toEqual([]);
  });

  it('keeps a useful suggestion on the screen and marks it', async () => {
    await recommendations.load('prj_1');
    await recommendations.feedback(rec(idOf(1)), 'useful');
    expect(recommendations.shown('prj_1')).toHaveLength(1);
    expect(recommendations.given[idOf(1)]).toBe('useful');
  });

  it('is kept on the device only where memory is not served (a phone) and for a connected workspace', async () => {
    server.set = set([rec(idOf(2))], { feedbackStoredIn: 'device' });
    await recommendations.load('prj_1', true);
    await recommendations.feedback(rec(idOf(2)), 'not_useful');
    expect(server.feedback).toEqual([]);
    expect(recommendations.shown('prj_1')).toEqual([]);
    await recommendations.feedback(rec(idOf(3), 'workspace:team:main/p1'), 'useful');
    expect(server.feedback).toEqual([]);
  });

  it('still hides the suggestion when memory could not take the verdict, and says so', async () => {
    await recommendations.load('prj_1');
    server.feedbackFails = true;
    await recommendations.feedback(rec(idOf(1)), 'not_useful');
    expect(recommendations.shown('prj_1')).toEqual([]);
    expect(recommendations.failed[idOf(1)]).toBe('memory is full');
    expect(recommendations.sending[idOf(1)]).toBeUndefined();
  });

  it('is counted once however often the button is pressed', async () => {
    await recommendations.load('prj_1');
    await Promise.all([recommendations.feedback(rec(idOf(1)), 'useful'), recommendations.feedback(rec(idOf(1)), 'useful')]);
    expect(server.feedback).toHaveLength(1);
  });
});
