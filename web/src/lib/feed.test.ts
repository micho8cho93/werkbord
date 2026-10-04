import { describe, expect, it } from 'vitest';
import { FeedBuilder, withoutBlockerReport } from './feed';
import type { ControllerEvent, Question } from './types';

let seq = 0;
function ev(type: string, payload: unknown, at = '2026-10-04T10:00:00Z'): ControllerEvent {
  return { seq: ++seq, type, runId: 'run_1', payload, createdAt: at };
}
const out = (stream: string, text: string) => ev('agent.output', { stream, text });
const question = (over: Partial<Question> = {}): Question => ({
  id: 'qst_1', runId: 'run_1', taskId: 'tsk_1', projectId: 'prj_1', kind: 'selection', prompt: 'Which one?', allowFreeText: true,
  state: 'pending', askedAt: '2026-10-04T10:00:00Z', ...over,
});

function build(events: ControllerEvent[]): FeedBuilder {
  const b = new FeedBuilder();
  b.apply(events);
  return b;
}

describe('FeedBuilder', () => {
  it('shows what the agent and the user said as messages', () => {
    const b = build([out('assistant', 'Looking at it.'), out('user', 'Use Postgres.')]);
    expect(b.items.map((i) => i.kind === 'message' && [i.role, i.text])).toEqual([
      ['assistant', 'Looking at it.'],
      ['user', 'Use Postgres.'],
    ]);
  });

  it('folds a run of tool use into one item, and starts a new one after speech', () => {
    const b = build([
      out('tool', 'Read a.go'),
      out('tool', 'Edit a.go'),
      out('tool', 'Bash: go test'),
      out('assistant', 'Tests pass.'),
      out('tool', 'Edit b.go'),
    ]);
    expect(b.items.map((i) => i.kind)).toEqual(['tools', 'message', 'tools']);
    const first = b.items[0];
    expect(first.kind === 'tools' && first.lines.map((l) => l.text)).toEqual(['Read a.go', 'Edit a.go', 'Bash: go test']);
  });

  it('keeps diagnostics out of the way, grouped', () => {
    const b = build([out('stderr', 'warn 1'), out('stderr', 'warn 2'), out('assistant', 'ok')]);
    expect(b.items.map((i) => i.kind)).toEqual(['diagnostics', 'message']);
    expect(b.items[0].kind === 'diagnostics' && b.items[0].lines).toHaveLength(2);
  });

  it('skips blank output and shows system notices', () => {
    const b = build([out('assistant', '   \n'), out('system', 'Session started with sonnet')]);
    expect(b.items).toHaveLength(1);
    expect(b.items[0]).toMatchObject({ kind: 'notice', text: 'Session started with sonnet' });
  });

  it('marks the lifecycle: started, finished, failed, stopped', () => {
    const b = build([
      ev('agent.started', { resumed: false }),
      ev('agent.started', { resumed: true }),
      ev('agent.completed', {}),
      ev('agent.failed', { reason: 'exited with status 2' }),
      ev('agent.stopped', { reason: 'stopped by user' }),
    ]);
    expect(b.items.map((i) => i.kind === 'marker' && [i.text, i.tone, i.detail])).toEqual([
      ['Agent started', 'info', undefined],
      ['Session resumed', 'info', undefined],
      ['Session finished', 'ok', undefined],
      ['Failed', 'bad', 'exited with status 2'],
      ['Stopped', 'neutral', 'stopped by user'],
    ]);
  });

  it('says nothing for an ordinary turn end, but explains an interruption', () => {
    const b = build([ev('agent.waiting', { reason: 'turn_complete' }), ev('agent.waiting', { reason: 'interrupted' }), ev('agent.resumed', { via: 'message' })]);
    expect(b.items).toHaveLength(1);
    expect(b.items[0]).toMatchObject({ kind: 'marker', text: 'Session interrupted' });
  });

  it('shows a question and then what became of it', () => {
    const b = build([ev('agent.question', { question: question() })]);
    expect(b.items[0]).toMatchObject({ kind: 'question', question: { state: 'pending' } });

    b.apply([ev('question.answered', { question: question({ state: 'answered', answer: 'A' }) })]);
    expect(b.items[0]).toMatchObject({ kind: 'question', question: { state: 'answered', answer: 'A' } });

    const c = build([ev('agent.question', { question: question({ id: 'qst_2' }) })]);
    c.apply([ev('question.cancelled', { question: question({ id: 'qst_2', state: 'cancelled', cancelReason: 'run_ended' }) })]);
    expect(c.items[0]).toMatchObject({ question: { state: 'cancelled', cancelReason: 'run_ended' } });
  });

  it('keeps a question that was answered but never delivered, with the answer the user gave', () => {
    const b = build([ev('agent.question', { question: question() })]);
    b.apply([ev('question.answered', { question: question({ state: 'answered', answer: 'SQLite' }) })]);
    b.apply([ev('question.cancelled', { question: question({ state: 'cancelled', answer: 'SQLite', cancelReason: 'run_ended' }) })]);
    expect(b.items).toHaveLength(1);
    expect(b.items[0]).toMatchObject({ question: { state: 'cancelled', answer: 'SQLite' } });
  });

  it('shows several questions of one run as separate items, each closing on its own', () => {
    const b = build([
      ev('agent.question', { question: question({ id: 'a' }) }),
      ev('agent.question', { question: question({ id: 'b', kind: 'approval' }) }),
      ev('question.answered', { question: question({ id: 'b', kind: 'approval', state: 'answered', answer: 'Allow' }) }),
    ]);
    expect(b.items.map((i) => i.kind === 'question' && [i.question.id, i.question.state])).toEqual([
      ['a', 'pending'],
      ['b', 'answered'],
    ]);
  });

  it('ignores events that are not about the conversation', () => {
    const b = build([ev('run.state_changed', { run: {} }), ev('task.updated', {})]);
    expect(b.items).toEqual([]);
  });

  describe('replays and gaps', () => {
    it('applies each event once, however often it arrives', () => {
      const events = [out('assistant', 'one'), out('tool', 't1'), out('tool', 't2')];
      const b = build(events);
      expect(b.apply(events)).toBe(false);
      expect(b.apply([events[1]])).toBe(false);
      expect(b.items).toHaveLength(2);
      expect(b.items[1].kind === 'tools' && b.items[1].lines).toHaveLength(2);
    });

    it('reads in sequence order even if events arrive out of order', () => {
      const a = out('assistant', 'first');
      const b2 = out('assistant', 'second');
      const c = out('assistant', 'third');
      const b = build([c]);
      b.apply([a, b2]);
      expect(b.items.map((i) => i.kind === 'message' && i.text)).toEqual(['first', 'second', 'third']);
      expect(b.firstSeq).toBe(a.seq);
      expect(b.lastSeq).toBe(c.seq);
    });

    it('merges older history in front, regrouping tool use across the join', () => {
      const t1 = out('tool', 'old tool');
      const t2 = out('tool', 'new tool');
      const b = build([t2]);
      b.apply([t1]);
      expect(b.items).toHaveLength(1);
      expect(b.items[0].kind === 'tools' && b.items[0].lines.map((l) => l.text)).toEqual(['old tool', 'new tool']);
    });

    it('applies a question and its answer arriving in either order', () => {
      const q = ev('agent.question', { question: question({ id: 'qst_9' }) });
      const a = ev('question.answered', { question: question({ id: 'qst_9', state: 'answered', answer: 'yes' }) });
      const b = build([a]);
      b.apply([q]);
      expect(b.items[0]).toMatchObject({ kind: 'question', question: { state: 'answered', answer: 'yes' } });
    });

    it('never changes an item in place, so views can tell what changed', () => {
      const b = build([out('assistant', 'hello'), out('tool', 't1')]);
      const before = b.items;
      const [msg, tools] = before;
      b.apply([out('tool', 't2')]);
      expect(b.items).not.toBe(before);
      expect(b.items[0]).toBe(msg);
      expect(b.items[1]).not.toBe(tools);
      expect(tools.kind === 'tools' && tools.lines).toHaveLength(1);
    });
  });
});

describe('FeedBuilder and execution policy', () => {
  const blocker = { summary: 'Which payment provider?', source: 'question', raisedAt: '2026-10-04T10:00:00Z' };

  it('shows that the run stopped rather than guess, and why', () => {
    const b = build([out('assistant', 'Stopping here.'), ev('agent.blocked', { blocker })]);
    const last = b.items[b.items.length - 1];
    expect(last).toMatchObject({ kind: 'marker', tone: 'block', detail: 'Which payment provider?' });
    expect(last.kind === 'marker' && last.text).toMatch(/Blocked/);
  });

  it('says the user\'s message unblocked it, and only then', () => {
    const settled = build([ev('agent.blocked', { blocker }), out('user', 'Use Stripe.'), ev('agent.resumed', { via: 'message' })]);
    expect(settled.items.map((i) => i.kind === 'marker' && i.text)).toEqual([expect.stringMatching(/Blocked/), false, 'Unblocked by your message']);

    // An ordinary message to a run that was not blocked adds no marker.
    const plain = build([out('user', 'carry on'), ev('agent.resumed', { via: 'message' })]);
    expect(plain.items.map((i) => i.kind)).toEqual(['message']);

    // Nor does a second one, after the first has ended the block.
    const twice = build([ev('agent.blocked', { blocker }), ev('agent.resumed', { via: 'message' }), ev('agent.resumed', { via: 'message' })]);
    expect(twice.items.filter((i) => i.kind === 'marker' && i.text === 'Unblocked by your message')).toHaveLength(1);
  });

  it('still reads right when history arrives out of order', () => {
    const blockedEv = ev('agent.blocked', { blocker });
    const resumedEv = ev('agent.resumed', { via: 'message' });
    const b = new FeedBuilder();
    b.apply([resumedEv]);
    b.apply([blockedEv]); // older than what is shown: the feed is rebuilt in order
    expect(b.items.map((i) => i.kind === 'marker' && i.tone)).toEqual(['block', 'info']);
  });

  it('keeps a question the policy answered, marked as such', () => {
    const asked = question({ state: 'answered', answer: 'decide it yourself', answeredBy: 'policy' });
    const b = build([ev('agent.question', { question: asked }), ev('question.answered', { question: asked })]);
    expect(b.items).toHaveLength(1);
    const item = b.items[0];
    expect(item.kind === 'question' && [item.question.state, item.question.answeredBy]).toEqual(['answered', 'policy']);
  });

  it('does not show the blocker report as raw text, but keeps what was said around it', () => {
    const report = 'DEVBOARD_BLOCKED {"summary":"stuck"}';
    expect(build([out('assistant', report)]).items).toEqual([]);
    const b = build([out('assistant', `I compared both.\n${report}`)]);
    expect(b.items.map((i) => i.kind === 'message' && i.text)).toEqual(['I compared both.']);
    // Only the assistant's own report: a user who types the word is shown as typed.
    expect(build([out('user', report)]).items).toHaveLength(1);
    expect(withoutBlockerReport('nothing special')).toBe('nothing special');
    expect(withoutBlockerReport('mentions DEVBOARD_BLOCKED mid-line')).toBe('mentions DEVBOARD_BLOCKED mid-line');
  });
});
