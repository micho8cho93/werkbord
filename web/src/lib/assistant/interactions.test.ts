import { describe, expect, it } from 'vitest';
import { InteractionQueue, type Interaction } from './interactions';
import type { Question } from '../types';
const request = (id: string, projectId = 'p'): Interaction => ({ key: `question:${projectId}:${id}`, kind: 'question', question: {id, projectId, runId: `run-${id}`} as Question });
describe('shared interaction queue', () => {
  it('keeps four requests FIFO across replay, dismissal, new arrivals and resolution', () => {
    const queue = new InteractionQueue();
    const four = ['a', 'b', 'c', 'd'].map(id => request(id));
    expect(queue.reconcile(four).map(r => r.key)).toEqual(four.map(r => r.key));
    // Reconnecting or hiding the card must not replace its unanswered head.
    expect(queue.reconcile([...four].reverse().concat(request('e')))[0].key).toBe(four[0].key);
    expect(queue.reconcile(four.concat(request('e'))).length).toBe(5);
    for (let i = 1; i < 4; i++) expect(queue.reconcile(four.slice(i))[0].key).toBe(four[i].key);
    expect(queue.reconcile([])).toEqual([]);
  });
  it('deduplicates replayed questions and preserves source authority across question and proposal IDs', () => {
    const queue = new InteractionQueue();
    const a = request('same');
    const other = request('same', 'other');
    const proposal = {key:'proposal:session:same',kind:'proposal',item:{}} as Interaction;
    expect(queue.reconcile([a, a, other, proposal]).map(r => r.key)).toEqual([a.key, other.key, proposal.key]);
    expect(queue.reconcile([other, proposal])).toEqual([other, proposal]);
  });
});

it('merges recovered questions and Salie proposals by time without replacing an already displayed head', () => {
  const queue = new InteractionQueue();
  const q = request('question'); if (q.kind === 'question') q.question.askedAt = '2026-10-25T08:00:02Z';
  const proposal = {key:'proposal:session:older',kind:'proposal',item:{createdAt:'2026-10-25T08:00:01Z'}} as Interaction;
  expect(queue.reconcile([q,proposal]).map(r=>r.key)).toEqual([proposal.key,q.key]);
  const earlier = request('recovered'); if (earlier.kind === 'question') earlier.question.askedAt = '2026-10-25T08:00:00Z';
  expect(queue.reconcile([earlier,q,proposal])[0].key).toBe(proposal.key);
});
