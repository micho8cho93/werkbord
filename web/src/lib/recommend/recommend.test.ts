import { describe, expect, it } from 'vitest';
import { hide, isHidden } from './hidden';
import { BASIS_LABEL, CONFIDENCE_LABEL, INTENT_LABEL, TIER_LABEL, confirmationNote, deckHref, destination, evidenceMeta, feedbackNote, gaps, groupedByTier, isConnectedProject, sourceLabel, suggestedWorkflow } from './recommend';
import type { RecAction, RecActionKind, RecIntent, Recommendation, RecommendationSet } from './types';

const action = (kind: RecActionKind, over: Partial<RecAction> = {}): RecAction => ({ kind, label: 'Open', startsExecution: false, confirmation: 'at_destination', ...over });
const rec = (a: RecAction, projectId = 'prj_1'): Pick<Recommendation, 'projectId' | 'action'> => ({ projectId, action: a });

describe('where a suggestion leads', () => {
  it('only ever opens screens that exist, in the project the suggestion is about', () => {
    expect(destination(rec(action('open_task', { taskId: 'tsk_1' })))).toEqual({ href: '#/p/prj_1/task/tsk_1' });
    expect(destination(rec(action('open_branch', { branch: 'devboard/fix-1a2b3c' })))).toEqual({ href: '#/p/prj_1/git/branch/local/devboard%2Ffix-1a2b3c' });
    expect(destination(rec(action('open_changes')))).toEqual({ href: '#/p/prj_1/git/changes' });
    expect(destination(rec(action('open_git')))).toEqual({ href: '#/p/prj_1/git' });
    expect(destination(rec(action('open_health')))).toEqual({ href: '#/p/prj_1/git' });
    expect(destination(rec(action('open_board')))).toEqual({ href: '#/p/prj_1/board' });
    expect(destination(rec(action('open_workflow', { workflow: 'find-bugs' })))).toEqual({ workflow: 'find-bugs' });
  });

  it('has nowhere to go when the suggestion names nothing to open', () => {
    expect(destination(rec(action('open_task')))).toBeNull();
    expect(destination(rec(action('open_workflow')))).toBeNull();
    expect(destination(rec(action('open_branch')))).toEqual({ href: '#/p/prj_1/git' });
  });

  it('never builds an address from text a person or a model wrote', () => {
    const d = destination(rec(action('open_task', { taskId: '../../settings?token=x' })));
    expect(d).toEqual({ href: expect.stringMatching(/^#\/p\/prj_1\/task\//) });
    expect(JSON.stringify(d)).not.toContain('?token');
  });

  it('leaves a connected workspace’s project to that workspace', () => {
    expect(isConnectedProject('workspace:team:main/p1')).toBe(true);
    expect(destination(rec(action('open_task', { taskId: 'workspace:team:main/t1' }), 'workspace:team:main/p1'))).toBeNull();
  });

  it('points a workflow at the deck without starting it', () => {
    expect(deckHref('refactor-codebase')).toBe('#/deck?suggest=refactor-codebase');
    expect(suggestedWorkflow('#/deck?suggest=find-bugs')).toBe('find-bugs');
    expect(suggestedWorkflow('#/deck?suggest=refactor-codebase')).toBe('refactor-codebase');
    for (const bad of ['#/deck', '#/deck?suggest=rm -rf', '#/deck?suggest=security-audit', '#/deck?suggest=', '']) expect(suggestedWorkflow(bad)).toBe('');
  });
});

describe('what the person is told', () => {
  it('says who decides', () => {
    expect(confirmationNote(rec(action('open_branch')))).toBe('Nothing changes until you confirm there.');
    expect(confirmationNote(rec(action('open_workflow', { workflow: 'find-bugs' })))).toBe('You start it yourself from the Control Deck.');
    expect(confirmationNote(rec(action('open_task', { confirmation: 'none' })))).toBe('');
  });

  it('names every kind of suggestion and every reason in a few words', () => {
    const intents: RecIntent[] = ['continue_work', 'prepare_integration', 'review_issues', 'find_bugs', 'refactor_codebase', 'next_tasks'];
    for (const i of intents) expect(INTENT_LABEL[i].length).toBeGreaterThan(0);
    expect(BASIS_LABEL.required).toBe('Needed first');
    expect(Object.values(TIER_LABEL)).toEqual(['App limit', 'Policy', 'Your decision', 'Current state', 'Habit']);
    expect(CONFIDENCE_LABEL.medium).toBe('Part of this is inferred from how you work');
    expect(sourceLabel('team_memory')).toBe('Team memory');
    expect(sourceLabel('something_new')).toBe('something new');
  });

  it('shows the strongest evidence first', () => {
    const groups = groupedByTier([{ tier: 5 as const, x: 'h' }, { tier: 2 as const, x: 'p' }, { tier: 4 as const, x: 'g' }, { tier: 2 as const, x: 'q' }]);
    expect(groups.map((g) => g.label)).toEqual(['Policy', 'Current state', 'Habit']);
    expect(groups[0].items.map((i) => i.x)).toEqual(['p', 'q']);
  });

  it('puts a fact’s source, record and limits in one line', () => {
    expect(evidenceMeta({ source: 'git', ref: 'main', verified: true })).toBe('Git · main');
    expect(evidenceMeta({ source: 'individual_memory', ref: 'mem_1', verified: false, note: 'freshness uncertain' })).toBe('Your memory · mem_1 · not verified · freshness uncertain');
    expect(evidenceMeta({ source: 'task', verified: true })).toBe('Task');
  });

  it('reports a gap as a gap', () => {
    const set = { sources: { teamMemory: 'unavailable' }, notes: ['Nothing is saved for this project yet.'] } as unknown as RecommendationSet;
    expect(gaps(set)).toEqual(['Nothing is saved for this project yet.', 'Team policies could not be read just now.']);
    expect(gaps({ sources: { teamMemory: 'not_checked' }, notes: [] } as unknown as RecommendationSet)).toEqual(['Team policies were not read here.']);
    expect(gaps({ sources: { teamMemory: 'read' }, notes: [] } as unknown as RecommendationSet)).toEqual([]);
  });

  it('is plain about where feedback goes and what it cannot do', () => {
    expect(feedbackNote('memory')).toContain('never becomes a rule');
    expect(feedbackNote('memory')).toContain('never shared with Team');
    expect(feedbackNote('device')).toContain('this device only');
  });
});

describe('suggestions hidden on this device', () => {
  const memory = (): Storage => {
    const m = new Map<string, string>();
    return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v), removeItem: (k) => void m.delete(k), clear: () => m.clear(), key: () => null, length: 0 };
  };
  const id = (n: number) => `rec_${n.toString(16).padStart(20, '0')}`;

  it('hides a suggestion for two weeks and then lets it return', () => {
    const s = memory();
    hide(id(1), 1000, s);
    expect(isHidden(id(1), 2000, s)).toBe(true);
    expect(isHidden(id(1), 1000 + 14 * 24 * 3600 * 1000 - 1, s)).toBe(true);
    expect(isHidden(id(1), 1000 + 14 * 24 * 3600 * 1000 + 1, s)).toBe(false);
    expect(isHidden(id(2), 2000, s)).toBe(false);
  });

  it('keeps only the newest fifty', () => {
    const s = memory();
    for (let i = 0; i < 70; i++) hide(id(i), 1000 + i, s);
    expect(isHidden(id(69), 2000, s)).toBe(true);
    expect(isHidden(id(0), 2000, s)).toBe(false);
    expect(Object.keys(JSON.parse(s.getItem('werkbord.recommendations.hidden') ?? '{}'))).toHaveLength(50);
  });

  it('ignores what is not an id, and survives damaged or missing storage', () => {
    const s = memory();
    hide('not-an-id', 1000, s);
    hide('rec_../../x', 1000, s);
    expect(s.getItem('werkbord.recommendations.hidden')).toBeNull();
    s.setItem('werkbord.recommendations.hidden', '[1,2');
    expect(isHidden(id(1), 1000, s)).toBe(false);
    s.setItem('werkbord.recommendations.hidden', JSON.stringify({ [id(1)]: 'soon', 'rec_x': 5e12, [id(2)]: 5e12 }));
    expect(isHidden(id(1), 1000, s)).toBe(false);
    expect(isHidden(id(2), 1000, s)).toBe(true);
    expect(isHidden(id(1), 1000, null)).toBe(false);
    expect(() => hide(id(1), 1000, null)).not.toThrow();
    const full = { ...memory(), setItem: () => { throw new Error('quota'); } } as Storage;
    expect(() => hide(id(1), 1000, full)).not.toThrow();
  });
});
