import { describe, expect, it } from 'vitest';
import { ApiError } from './api';
import {
  QuestionBook,
  answerLine,
  choicesOf,
  closedText,
  contextIsLong,
  contextSummary,
  describeAnswerFailure,
  freeTextHint,
  isAllow,
  isDeny,
  kindLabel,
  needsInputText,
  pageTitle,
  takesText,
} from './questions';
import type { ControllerEvent, Question } from './types';

let seq = 0;
const q = (over: Partial<Question> = {}): Question => ({
  id: 'qst_1', runId: 'run_1', taskId: 'tsk_1', projectId: 'prj_1', kind: 'clarification', prompt: 'Why?', allowFreeText: true,
  state: 'pending', askedAt: '2026-10-04T10:00:00.000Z', ...over,
});
const ev = (type: string, question: Question): ControllerEvent => ({ seq: ++seq, type, runId: question.runId, payload: { question }, createdAt: question.askedAt });

describe('how questions are described', () => {
  it('names every kind in the words of what the agent wants', () => {
    expect(kindLabel('approval')).toBe('Needs your approval');
    expect(kindLabel('decision')).toBe('Needs your decision');
    expect(kindLabel('selection')).toBe('Needs a choice');
    expect(kindLabel('instruction')).toBe('Needs instructions');
    expect(kindLabel('clarification')).toBe('Needs more information');
  });

  it('offers the options as choices, and Allow/Deny for an approval that came without any', () => {
    expect(choicesOf(q({ options: ['A', 'B'] }))).toEqual(['A', 'B']);
    expect(choicesOf(q({ kind: 'approval' }))).toEqual(['Allow', 'Deny']);
    expect(choicesOf(q())).toEqual([]);
  });

  it('takes typed text only where the controller will accept it', () => {
    expect(takesText(q({ options: ['A', 'B'], allowFreeText: false }))).toBe(false); // an approval: Allow or Deny, nothing else
    expect(takesText(q({ options: ['A', 'B'], allowFreeText: true }))).toBe(true);
    expect(takesText(q({ allowFreeText: false }))).toBe(true); // nothing to tap, so there must be a box
  });

  it('words the box for what is being asked', () => {
    expect(freeTextHint(q({ options: ['A'] }))).toBe('Or write your own answer');
    expect(freeTextHint(q({ kind: 'instruction' }))).toBe('Tell the agent what to do next');
    expect(freeTextHint(q({ kind: 'decision' }))).toBe('Your decision');
    expect(freeTextHint(q())).toBe('Your answer');
  });

  it('tells grants from refusals', () => {
    for (const o of ['Allow', 'allow', 'Yes', 'Approve', 'Proceed with the plan']) expect(isAllow(o)).toBe(true);
    for (const o of ['Deny', 'No', 'Decline', 'Cancel']) expect(isDeny(o)).toBe(true);
    expect(isAllow('Allow for this session')).toBe(true);
    expect(isAllow('Postgres')).toBe(false);
    expect(isDeny('Notes')).toBe(false); // "No" at the start of a word is not a refusal
  });

  it('folds long context away, so choices stay on screen', () => {
    expect(contextIsLong('$ npm test')).toBe(false);
    expect(contextIsLong('x'.repeat(400))).toBe(true);
    expect(contextIsLong('1\n2\n3\n4\n5\n6\n7\n8')).toBe(true);
    expect(contextSummary('one line')).toBe('Details');
    expect(contextSummary('a\nb\nc')).toBe('Details (3 lines)');
  });

  it('explains why a question was closed', () => {
    expect(closedText(q({ state: 'cancelled', cancelReason: 'run_ended' }))).toMatch(/agent stopped/);
    expect(closedText(q({ state: 'cancelled', cancelReason: 'interrupted' }))).toMatch(/Send a message to continue/);
    expect(closedText(q({ state: 'cancelled', cancelReason: 'withdrawn' }))).toMatch(/no longer needs an answer/);
    expect(closedText(q({ state: 'cancelled', cancelReason: 'run_ended', answer: 'yes' }))).toMatch(/Your answer was not delivered/);
  });

  it('puts the count in the tab title and the banner', () => {
    expect(pageTitle(0)).toBe('Werkbord');
    expect(pageTitle(3)).toBe('(3) Needs input · Werkbord');
    expect(needsInputText(1)).toBe('1 question needs your input');
    expect(needsInputText(2)).toBe('2 questions need your input');
  });
});

describe('what the user is told when an answer does not go through', () => {
  it('says another device got there first, with the answer it gave', () => {
    const err = new ApiError(409, 'question_answered', 'x', q({ state: 'answered', answer: 'SQLite' }));
    expect(describeAnswerFailure(err)).toEqual({ message: 'Already answered: “SQLite”. Nothing was sent again.', settled: true, notice: true });
  });

  it('says the agent moved on, and stops offering the question', () => {
    const err = new ApiError(409, 'question_closed', 'x', q({ state: 'cancelled', cancelReason: 'run_ended' }));
    const f = describeAnswerFailure(err);
    expect(f.settled).toBe(true);
    expect(f.message).toMatch(/agent stopped/);
  });

  it('keeps the question, and the typed answer, when the controller cannot be reached', () => {
    const f = describeAnswerFailure(new ApiError(0, 'offline', 'Cannot reach the controller.'));
    expect(f.settled).toBe(false);
    expect(f.notice).toBe(false);
    expect(f.message).toMatch(/safe to try again/);
  });

  it('shows a refused answer inline, and does not drop the question', () => {
    const f = describeAnswerFailure(new ApiError(400, 'invalid', 'the answer must be one of: Allow, Deny'));
    expect(f).toEqual({ message: 'the answer must be one of: Allow, Deny', settled: false, notice: false });
  });

  it('drops a question the controller no longer knows', () => {
    expect(describeAnswerFailure(new ApiError(404, 'not_found', 'x')).settled).toBe(true);
  });

  it('copes with errors of other kinds', () => {
    expect(describeAnswerFailure(new Error('boom')).message).toBe('boom');
    expect(describeAnswerFailure('odd').message).toBe('odd');
  });
});

describe('QuestionBook', () => {
  it('lists the questions that wait, longest first', () => {
    const b = new QuestionBook();
    b.ask(q({ id: 'late', askedAt: '2026-10-04T10:05:00.000Z' }));
    b.ask(q({ id: 'early', askedAt: '2026-10-04T10:01:00.000Z' }));
    b.ask(q({ id: 'tie-b', askedAt: '2026-10-04T10:03:00.000Z' }));
    b.ask(q({ id: 'tie-a', askedAt: '2026-10-04T10:03:00.000Z' }));
    expect(b.pending.map((x) => x.id)).toEqual(['early', 'tie-a', 'tie-b', 'late']);
  });

  it('follows the event stream: asked, then answered or cancelled', () => {
    const b = new QuestionBook();
    expect(b.apply(ev('agent.question', q({ id: 'a' })))).toBe(true);
    expect(b.apply(ev('agent.question', q({ id: 'b' })))).toBe(true);
    expect(b.pending).toHaveLength(2);
    expect(b.apply(ev('question.answered', q({ id: 'a', state: 'answered', answer: 'x' })))).toBe(true);
    expect(b.apply(ev('question.cancelled', q({ id: 'b', state: 'cancelled', cancelReason: 'withdrawn' })))).toBe(true);
    expect(b.pending).toEqual([]);
  });

  it('ignores events that are not about questions, and the same event twice', () => {
    const b = new QuestionBook();
    expect(b.apply({ seq: 1, type: 'run.state_changed', payload: { run: {} }, createdAt: '' })).toBe(false);
    expect(b.apply({ seq: 2, type: 'task.updated', createdAt: '' })).toBe(false);
    const asked = ev('agent.question', q());
    b.apply(asked);
    b.apply(asked);
    expect(b.pending).toHaveLength(1);
  });

  it('never shows a question again once it was seen closed, however late a list arrives', () => {
    const b = new QuestionBook();
    const sync = b.beginSync(); // a list is requested...
    b.apply(ev('question.answered', q({ id: 'a', state: 'answered', answer: 'x' }))); // ...a question is answered meanwhile...
    expect(b.replace(sync, [q({ id: 'a' }), q({ id: 'b' })])).toBe(true); // ...and the list, taken earlier, still has it
    expect(b.pending.map((x) => x.id)).toEqual(['b']);
    b.apply(ev('agent.question', q({ id: 'a' }))); // a replayed ask cannot bring it back either
    expect(b.pending.map((x) => x.id)).toEqual(['b']);
  });

  it('does not lose a question asked while a list was in flight', () => {
    const b = new QuestionBook();
    const sync = b.beginSync();
    b.apply(ev('agent.question', q({ id: 'new' }))); // asked after the list was taken
    b.replace(sync, [q({ id: 'old' })]);
    expect(b.pending.map((x) => x.id).sort()).toEqual(['new', 'old']);
  });

  it('treats the fetched list as the truth for what it names', () => {
    const b = new QuestionBook();
    b.ask(q({ id: 'stale' })); // asked, then answered on a device this one never heard from
    b.replace(b.beginSync(), [q({ id: 'live' })]);
    expect(b.pending.map((x) => x.id)).toEqual(['live']);
  });

  it('drops a list that was overtaken by a newer request', () => {
    const b = new QuestionBook();
    const first = b.beginSync();
    const second = b.beginSync();
    expect(b.replace(first, [q({ id: 'old' })])).toBe(false);
    expect(b.replace(second, [q({ id: 'new' })])).toBe(true);
    expect(b.pending.map((x) => x.id)).toEqual(['new']);
  });

  it('lets the user close a question at once, without waiting for its event', () => {
    const b = new QuestionBook();
    b.ask(q({ id: 'a' }));
    b.resolve({ id: 'a' });
    expect(b.pending).toEqual([]);
    b.apply(ev('question.answered', q({ id: 'a', state: 'answered', answer: 'x' }))); // the event follows: no harm
    expect(b.pending).toEqual([]);
  });

  it('does not take in a question that is not pending', () => {
    const b = new QuestionBook();
    b.ask(q({ state: 'answered', answer: 'x' }));
    b.replace(b.beginSync(), [q({ id: 'c', state: 'cancelled' })]);
    expect(b.pending).toEqual([]);
  });
});

describe('answerLine', () => {
  const q = (over: Partial<Question>): Question => ({
    id: 'q', runId: 'r', taskId: 't', projectId: 'p', kind: 'clarification', prompt: 'Which?', allowFreeText: true,
    state: 'answered', answer: 'Use Postgres', askedAt: '2026-10-04T10:00:00Z', ...over,
  });

  it('says it was the user who answered, unless the run\'s policy did', () => {
    expect(answerLine(q({ answeredBy: 'user' }))).toBe('You answered: Use Postgres');
    const policy = answerLine(q({ answeredBy: 'policy', answer: 'Decide this yourself.' }));
    expect(policy).toMatch(/You were not asked/);
    expect(policy).not.toMatch(/Decide this yourself/); // the agent's instruction is not put in the user's mouth
  });
});
