import { describe, expect, it } from 'vitest';
import { FeedBuilder } from './feed';
import type { ControllerEvent, Question } from './types';

let seq = 0;
function ev(type: string, payload: unknown, at = '2026-10-04T10:00:00Z'): ControllerEvent {
  return { seq: ++seq, type, runId: 'run_1', payload, createdAt: at };
}
const out = (stream: string, text: string) => ev('agent.output', { stream, text });
const question = (over: Partial<Question> = {}): Question => ({
  id: 'qst_1', runId: 'run_1', kind: 'ask', prompt: 'Which one?', status: 'pending', createdAt: '2026-10-04T10:00:00Z', ...over,
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
    expect(b.items[0]).toMatchObject({ kind: 'question', question: { status: 'pending' } });

    b.apply([ev('question.answered', { question: question({ status: 'answered', answer: 'A' }) })]);
    expect(b.items[0]).toMatchObject({ kind: 'question', question: { status: 'answered', answer: 'A' } });

    const c = build([ev('agent.question', { question: question({ id: 'qst_2' }) })]);
    c.apply([ev('question.answered', { question: question({ id: 'qst_2', status: 'cancelled' }) })]);
    expect(c.items[0]).toMatchObject({ question: { status: 'cancelled' } });
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
      const a = ev('question.answered', { question: question({ id: 'qst_9', status: 'answered', answer: 'yes' }) });
      const b = build([a]);
      b.apply([q]);
      expect(b.items[0]).toMatchObject({ kind: 'question', question: { status: 'answered', answer: 'yes' } });
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
