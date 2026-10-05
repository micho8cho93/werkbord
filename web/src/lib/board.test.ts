import { describe, expect, it } from 'vitest';
import { cardStatus, dropAction, quickChoices, sinceShort } from './board';
import type { Question, Run, Task } from './types';

const now = Date.parse('2026-10-05T10:30:00Z');

const task = (o: Partial<Task> = {}): Task => ({
  id: 'tsk_1',
  projectId: 'prj_1',
  title: 'Fix flaky auth test',
  description: '',
  state: 'backlog',
  position: 1,
  execution: {},
  version: 1,
  createdAt: '2026-10-05T09:00:00Z',
  updatedAt: '2026-10-05T09:00:00Z',
  ...o,
});

const run = (o: Partial<Run> = {}): Run => ({
  id: 'run_1',
  taskId: 'tsk_1',
  projectId: 'prj_1',
  agentId: 'claude',
  state: 'running',
  policy: { interaction: 'interactive' },
  version: 1,
  createdAt: '2026-10-05T10:18:00Z',
  updatedAt: '2026-10-05T10:18:00Z',
  ...o,
});

const ctx = { now, questions: [] as Question[], waitingFor: [] as string[] };

describe('dropAction', () => {
  it('starts an agent when a backlog card is dropped in Doing', () => {
    expect(dropAction(task(), undefined, 'doing', false)).toBe('start');
    expect(dropAction(task(), run({ state: 'failed' }), 'doing', false)).toBe('start');
  });

  it('only moves a card whose agent is already working', () => {
    expect(dropAction(task(), run(), 'doing', false)).toBe('move');
  });

  it('merges a reviewed branch on the way to Done, and moves work with nothing to merge', () => {
    expect(dropAction(task({ state: 'review' }), run({ state: 'completed' }), 'done', true)).toBe('merge');
    expect(dropAction(task({ state: 'review' }), run({ state: 'completed' }), 'done', false)).toBe('move');
  });

  it('does nothing on its own column and moves everywhere else', () => {
    expect(dropAction(task(), undefined, 'backlog', false)).toBe('none');
    expect(dropAction(task({ state: 'doing' }), undefined, 'review', false)).toBe('move');
    expect(dropAction(task({ state: 'done' }), undefined, 'backlog', false)).toBe('move');
  });
});

describe('cardStatus', () => {
  it('says how long a run has been working, and where', () => {
    const s = cardStatus(task({ state: 'doing' }), run({ activity: 'editing session_test.go' }), { ...ctx, runnerName: 'this computer' });
    expect(s).toEqual({ tone: 'work', text: 'Running · 12m 00s · this computer', detail: '› editing session_test.go', live: true });
  });

  it('puts the question first when the agent asks one', () => {
    const q = { id: 'q1', prompt: 'Run `go test ./...`?', askedAt: '2026-10-05T10:26:00Z' } as Question;
    const s = cardStatus(task({ state: 'doing' }), run({ state: 'waiting_for_user', waiting: 'question' }), { ...ctx, questions: [q] });
    expect(s.tone).toBe('ask');
    expect(s.text).toBe('Needs you · 4m');
    expect(s.detail).toBe('Run `go test ./...`?');
  });

  it('describes work ready for review by its branch', () => {
    const s = cardStatus(task({ state: 'review' }), run({ state: 'completed', branch: 'werkbord/fix-auth' }), ctx);
    expect(s).toMatchObject({ tone: 'ok', text: 'Ready for review', detail: 'werkbord/fix-auth' });
  });

  it('names what a task waits for before saying it has not started', () => {
    expect(cardStatus(task(), undefined, { ...ctx, waitingFor: ['Fix flaky auth test'] }).text).toBe('Waits for Fix flaky auth test');
    expect(cardStatus(task(), undefined, ctx).text).toBe('Not started');
  });
});

describe('quickChoices', () => {
  it('offers short choices on the card and leaves long ones to the task', () => {
    expect(quickChoices(['Allow', 'Deny'])).toEqual(['Allow', 'Deny']);
    expect(quickChoices(['a', 'b', 'c', 'd'])).toEqual([]);
    expect(quickChoices(['Use the local time zone of the controller machine'])).toEqual([]);
  });
});

describe('sinceShort', () => {
  it('rounds down to the largest unit', () => {
    expect(sinceShort('2026-10-05T10:29:30Z', now)).toBe('30s');
    expect(sinceShort('2026-10-05T07:30:00Z', now)).toBe('3h');
    expect(sinceShort('2026-10-03T10:30:00Z', now)).toBe('2d');
  });
});
