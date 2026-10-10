import { describe, expect, it } from 'vitest';
import { activeReply, adoptPending, apply, emptyTranscript, lookedAt, progressLine, reconcileSeq, restore, serialize, settleStale, stepLabel, type ConfirmItem, type ReplyItem } from './transcript';
import type { AssistantEvent } from './types';

let seq = 0;
const ev = (type: AssistantEvent['type'], more: Partial<AssistantEvent> = {}): AssistantEvent => ({ seq: ++seq, sessionId: 'ast_1', turnId: 'trn_1', type, at: '2026-03-09T09:00:00Z', ...more });
const fresh = () => {
  seq = 0;
  return emptyTranscript();
};
const reply = (t: ReturnType<typeof emptyTranscript>) => t.items.find((i): i is ReplyItem => i.kind === 'reply')!;
const confirm = (t: ReturnType<typeof emptyTranscript>) => t.items.find((i): i is ConfirmItem => i.kind === 'confirm')!;

describe('a reply streaming in', () => {
  it('grows word by word and ends done', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    expect(reply(t).status).toBe('working');
    apply(t, ev('delta', { text: 'Hel', attempt: 1 }));
    apply(t, ev('delta', { text: 'lo', attempt: 1 }));
    expect(reply(t).text).toBe('Hello');
    expect(activeReply(t)).toBe(reply(t));
    apply(t, ev('turn_completed', { state: 'idle' }));
    expect(reply(t).status).toBe('done');
    expect(activeReply(t)).toBeUndefined();
  });

  it('puts the words of the next trip to the provider after the earlier ones, as a new paragraph', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('delta', { text: 'Let me check.', attempt: 1 }));
    apply(t, ev('tool_call', { tool: { callId: 'c1', name: 'list_tickets' } }));
    apply(t, ev('tool_result', { tool: { callId: 'c1', name: 'list_tickets', status: 'ok' } }));
    apply(t, ev('delta', { text: 'Two are blocked.', attempt: 2 }));
    expect(reply(t).text).toBe('Let me check.\n\nTwo are blocked.');
  });

  it('discards what a failed attempt said when the retry begins, and only that', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('delta', { text: 'First round. ', attempt: 1 }));
    apply(t, ev('delta', { text: 'Second round, started', attempt: 2 }));
    expect(reply(t).text).toBe('First round. \n\nSecond round, started');
    apply(t, ev('notice', { text: 'The provider stopped. Trying again (3 of 3).' }));
    apply(t, ev('retry', { attempt: 3 }));
    expect(reply(t).text).toBe('First round. \n\n');
    apply(t, ev('delta', { text: 'Second round, done.', attempt: 3 }));
    expect(reply(t).text).toBe('First round. \n\nSecond round, done.');
    expect(reply(t).notices).toEqual(['The provider stopped. Trying again (3 of 3).']);
  });

  it('shows a retry of the very first attempt as a clean slate', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('delta', { text: 'garbled', attempt: 1 }));
    apply(t, ev('retry', { attempt: 2 }));
    apply(t, ev('delta', { text: 'Good.', attempt: 2 }));
    expect(reply(t).text).toBe('Good.');
  });
});

describe('events that arrive twice or late', () => {
  it('ignores an event it already has, so a replay changes nothing', () => {
    const t = fresh();
    const a = ev('turn_started');
    const b = ev('delta', { text: 'Hi', attempt: 1 });
    apply(t, a);
    apply(t, b);
    expect(apply(t, b)).toBe('ignored');
    apply(t, a);
    expect(reply(t).text).toBe('Hi');
    expect(t.lastSeq).toBe(b.seq);
  });

  it('builds the reply even if it missed the start of the turn (a page that came back mid-reply)', () => {
    const t = fresh();
    apply(t, ev('delta', { text: 'Half a sentence', attempt: 2 }));
    expect(reply(t).text).toBe('Half a sentence');
    apply(t, ev('turn_completed'));
    expect(reply(t).status).toBe('done');
  });

  it('reports a gap instead of guessing', () => {
    const t = fresh();
    expect(apply(t, { seq: 0, sessionId: 'ast_1', type: 'gap', at: '' })).toBe('gap');
  });

  it('counts again from the start after the server was restarted', () => {
    const t = fresh();
    t.lastSeq = 57;
    expect(reconcileSeq(t, 0)).toBe(true);
    expect(t.lastSeq).toBe(0);
    expect(apply(t, { seq: 1, sessionId: 'ast_1', turnId: 'trn_9', type: 'turn_started', at: '' })).toBe('applied');
    expect(reconcileSeq(t, 5)).toBe(false);
  });
});

describe('what the assistant looked up', () => {
  it('tracks a step from running to done, and says what it is doing in plain words', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('tool_call', { tool: { callId: 'c1', name: 'list_blockers' } }));
    expect(progressLine(reply(t))).toBe('Looking for what is blocked…');
    apply(t, ev('tool_result', { tool: { callId: 'c1', name: 'list_blockers', status: 'ok' } }));
    expect(progressLine(reply(t))).toBe('Thinking…');
    apply(t, ev('delta', { text: 'None.', attempt: 2 }));
    expect(progressLine(reply(t))).toBe('Working…');
    apply(t, ev('turn_completed'));
    expect(progressLine(reply(t))).toBe('');
    expect(lookedAt(reply(t))).toEqual(['Looking for what is blocked']);
  });

  it('says a lookup that did not work, and never invents a name for an unknown operation', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('tool_call', { tool: { callId: 'c1', name: 'get_ticket' } }));
    apply(t, ev('tool_result', { tool: { callId: 'c1', name: 'get_ticket', status: 'error', code: 'not_found', detail: 'no such ticket' } }));
    apply(t, ev('turn_completed'));
    expect(lookedAt(reply(t))).toEqual(['Reading a task (did not work)']);
    expect(stepLabel('rm_rf')).toBe('Looking something up');
  });

  it('stops steps that were still running when the turn ended without finishing', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('tool_call', { tool: { callId: 'c1', name: 'list_tickets' } }));
    apply(t, ev('turn_cancelled'));
    expect(reply(t).status).toBe('cancelled');
    expect(reply(t).steps[0].status).toBe('stopped');
  });
});

describe('a change waiting for the person', () => {
  const proposal = { actionId: 'act_1', operation: 'create_ticket', summary: 'Create task "Fix login"', expiresAt: '2026-03-09T09:15:00Z' };

  it('appears when proposed, once, and settles with what became of it', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('confirmation_required', { proposal, actionId: 'act_1' }));
    apply(t, ev('confirmation_required', { proposal, actionId: 'act_1' }));
    expect(t.items.filter((i) => i.kind === 'confirm')).toHaveLength(1);
    expect(confirm(t).state).toBe('waiting');
    apply(t, ev('turn_completed'));
    expect(confirm(t).state).toBe('waiting');
    apply(t, ev('confirmation_resolved', { actionId: 'act_1', text: 'confirmed', outcome: 'Created tsk_9' }));
    expect(confirm(t)).toMatchObject({ state: 'confirmed', outcome: 'Created tsk_9' });
  });

  it.each([
    ['declined', 'declined'],
    ['expired', 'expired'],
    ['failed', 'failed'],
  ] as const)('shows %s as %s', (word, state) => {
    const t = fresh();
    apply(t, ev('confirmation_required', { proposal, actionId: 'act_1' }));
    apply(t, ev('confirmation_resolved', { actionId: 'act_1', text: word, outcome: 'because' }));
    expect(confirm(t).state).toBe(state);
  });

  it('is found again after a reload from what the server lists, and closed if it was settled meanwhile', () => {
    const t = fresh();
    adoptPending(t, [{ id: 'act_1', operation: 'update_ticket', summary: 'Change ticket', expiresAt: '2026-03-09T09:15:00Z' }]);
    expect(confirm(t)).toMatchObject({ id: 'act_1', state: 'waiting' });
    adoptPending(t, []);
    expect(confirm(t).state).toBe('gone');
    adoptPending(t, [{ id: 'act_1', operation: 'update_ticket', summary: 'Change ticket', expiresAt: '' }]);
    expect(confirm(t).state).toBe('waiting');
  });

  it('does not reopen one that was already settled', () => {
    const t = fresh();
    apply(t, ev('confirmation_required', { proposal, actionId: 'act_1' }));
    apply(t, ev('confirmation_resolved', { actionId: 'act_1', text: 'declined' }));
    adoptPending(t, []);
    expect(confirm(t).state).toBe('declined');
  });
});

describe('failures', () => {
  it('keeps the words of the error', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('turn_failed', { error: { code: 'model_unavailable', message: 'Not on your plan', retryable: false } }));
    expect(reply(t)).toMatchObject({ status: 'failed', error: { code: 'model_unavailable' } });
  });

  it('settles a reply that was working when the page went away and is not running now', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('delta', { text: 'Partial', attempt: 1 }));
    settleStale(t, true);
    expect(reply(t).status).toBe('working');
    settleStale(t, false);
    expect(reply(t).status).toBe('cancelled');
    expect(reply(t).notices.join(' ')).toContain('final result could not be recovered');
    apply(t, ev('turn_completed'));
    expect(reply(t).status).toBe('done');
    expect(reply(t).notices).toEqual([]);
  });

  it('recovers a missed failure after reconnecting, even if part of the reply was received', () => {
    const t = fresh();
    apply(t, ev('turn_started'));
    apply(t, ev('delta', { text: 'I will look that up.', attempt: 1 }));
    settleStale(t, false, 'rate_limited');
    expect(reply(t)).toMatchObject({ status: 'failed', error: { code: 'rate_limited' } });
    apply(t, ev('turn_failed', { error: { code: 'rate_limited', message: 'Your allowance resets tomorrow.', retryable: false } }));
    expect(reply(t).error?.message).toBe('Your allowance resets tomorrow.');
  });
});

describe('keeping the conversation for the life of the tab', () => {
  it('round-trips, and keeps only what it should', () => {
    const t = fresh();
    t.items.push({ kind: 'user', id: 'u1', text: 'hi', files: [{ name: 'a.txt', bytes: 3 }] });
    apply(t, ev('turn_started'));
    apply(t, ev('delta', { text: 'Hello', attempt: 1 }));
    apply(t, ev('turn_completed'));
    const back = restore(serialize('ast_abc', t))!;
    expect(back.sessionId).toBe('ast_abc');
    expect(back.lastSeq).toBe(t.lastSeq);
    expect(back.items).toHaveLength(2);
    expect(JSON.stringify(back)).not.toContain('content');
  });

  it('is bounded: a long conversation keeps its end, and stays small', () => {
    const t = fresh();
    for (let i = 0; i < 200; i++) t.items.push({ kind: 'user', id: 'u' + i, text: 'x'.repeat(2000), files: [] });
    const raw = serialize('ast_abc', t);
    expect(raw.length).toBeLessThan(130_000);
    const back = restore(raw)!;
    expect((back.items.at(-1) as { id: string }).id).toBe('u199');
  });

  it.each([
    ['nothing', null],
    ['not json', '{'],
    ['wrong version', JSON.stringify({ v: 2, sessionId: 'ast_a', lastSeq: 1, items: [] })],
    ['a session id that is not one', JSON.stringify({ v: 1, sessionId: '../../x', lastSeq: 1, items: [] })],
    ['no items', JSON.stringify({ v: 1, sessionId: 'ast_a', lastSeq: 1 })],
  ])('starts clean from %s', (_n, raw) => {
    expect(restore(raw)).toBeNull();
  });

  it('drops items of an unknown shape rather than drawing them', () => {
    const raw = JSON.stringify({ v: 1, sessionId: 'ast_a', lastSeq: 4, items: [{ kind: 'script', id: 'x', text: '<img onerror=1>' }, { kind: 'user', id: 'u', text: 'ok', files: [] }, 7, null] });
    expect(restore(raw)!.items).toEqual([{ kind: 'user', id: 'u', text: 'ok', files: [] }]);
  });
});
