import { afterEach, describe, expect, it, vi } from 'vitest';
import { backoff, Connection, SSEParser, type Link } from './stream';
import type { AssistantEvent } from './types';

afterEach(() => vi.useRealTimers());

describe('the event stream parser', () => {
  it('reads events however the bytes are cut', () => {
    const wire = ': ping\n\nid: 1\nevent: delta\ndata: {"a":1}\n\nid: 2\nevent: turn_completed\ndata: {"b":\ndata: 2}\n\n';
    for (let cut = 0; cut <= wire.length; cut++) {
      const p = new SSEParser();
      const got = [...p.feed(wire.slice(0, cut)), ...p.feed(wire.slice(cut))];
      expect(got.map((m) => [m.id, m.event, m.data])).toEqual([
        ['1', 'delta', '{"a":1}'],
        ['2', 'turn_completed', '{"b":\n2}'],
      ]);
    }
  });

  it('copes with CRLF, a CR alone, and a CR LF split between chunks', () => {
    const p = new SSEParser();
    const got = [...p.feed('event: a\r\ndata: 1\r\n\r'), ...p.feed('\nevent: b\rdata: 2\r\r'), ...p.feed('retry: 3000\n\n')];
    expect(got.map((m) => [m.event, m.data])).toEqual([
      ['a', '1'],
      ['b', '2'],
    ]);
  });

  it('ignores comments, the retry hint and a blank block, and defaults the event name', () => {
    const p = new SSEParser();
    expect(p.feed('retry: 3000\n\n: ping\n\n\n')).toEqual([]);
    expect(p.feed('data: x\n\n').map((m) => m.event)).toEqual(['message']);
  });
});

describe('waiting between attempts', () => {
  it('doubles from half a second to eight, and stays there', () => {
    expect([1, 2, 3, 4, 5, 6, 20].map(backoff)).toEqual([500, 1000, 2000, 4000, 8000, 8000, 8000]);
  });
});

/** A response whose body the test feeds by hand. */
function stream() {
  let push!: (s: string) => void;
  let end!: () => void;
  let fail!: (e: Error) => void;
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      const enc = new TextEncoder();
      push = (s) => c.enqueue(enc.encode(s));
      end = () => c.close();
      fail = (e) => c.error(e);
    },
  });
  return { response: new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }), push, end, fail };
}
const frame = (e: Partial<AssistantEvent> & { seq: number; type: AssistantEvent['type'] }) => `id: ${e.seq}\nevent: ${e.type}\ndata: ${JSON.stringify({ sessionId: 'ast_1', at: '', ...e })}\n\n`;
const tick = () => new Promise((r) => setTimeout(r, 0));
async function until(f: () => boolean) {
  for (let i = 0; i < 200 && !f(); i++) await tick();
  expect(f()).toBe(true);
}

describe('a connection to a conversation', () => {
  it('delivers events, and after it drops it comes back asking for exactly what it has not seen', async () => {
    const first = stream();
    const second = stream();
    const calls: Request[] = [];
    const queue = [first.response, second.response];
    let last = 0;
    const got: number[] = [];
    const links: Link[] = [];
    const c = new Connection({
      sessionId: 'ast_1',
      after: () => last,
      onEvent: (e) => {
        got.push(e.seq);
        last = e.seq;
      },
      onLink: (l) => links.push(l),
      fetchImpl: ((url: string, init: RequestInit) => {
        calls.push(new Request('http://x' + url, init));
        return Promise.resolve(queue.shift()!);
      }) as unknown as typeof fetch,
      sleep: () => Promise.resolve(),
    });
    c.start();
    await until(() => links.includes('live'));
    first.push(frame({ seq: 1, type: 'turn_started' }) + frame({ seq: 2, type: 'delta', text: 'Hi' }));
    await until(() => got.length === 2);
    first.end(); // the connection drops mid-turn
    await until(() => calls.length === 2);
    second.push(frame({ seq: 3, type: 'delta', text: ' there' }));
    await until(() => got.length === 3);
    c.stop();

    expect(calls[0].headers.get('Last-Event-ID')).toBeNull();
    expect(calls[1].headers.get('Last-Event-ID')).toBe('2');
    expect(calls[0].url).toBe('http://x/api/assistant/sessions/ast_1/events');
    expect(calls[0].headers.get('Accept')).toBe('text/event-stream');
    expect(got).toEqual([1, 2, 3]);
    expect(links).toContain('reconnecting');
    expect(links.at(-1)).toBe('idle');
  });

  it('reconciles with the server before any event is read, so a restarted server is counted right', async () => {
    const s = stream();
    const order: string[] = [];
    const c = new Connection({
      sessionId: 'ast_1',
      after: () => 57,
      onOpen: async () => {
        await tick();
        order.push('open');
      },
      onEvent: (e) => order.push('event ' + e.seq),
      onLink: (l) => l === 'live' && order.push('live'),
      fetchImpl: (() => Promise.resolve(s.response)) as unknown as typeof fetch,
      sleep: () => Promise.resolve(),
    });
    s.push(frame({ seq: 1, type: 'turn_started' }));
    c.start();
    await until(() => order.includes('event 1'));
    c.stop();
    expect(order).toEqual(['open', 'live', 'event 1']);
  });

  it.each([
    [401, 'unauthorized'],
    [403, 'unauthorized'],
    [404, 'gone'],
    [503, 'unavailable'],
  ] as const)('stops for good on %i and says %s', async (status, link) => {
    let n = 0;
    const links: Link[] = [];
    const c = new Connection({
      sessionId: 'ast_1',
      after: () => 0,
      onEvent: () => {},
      onLink: (l) => links.push(l),
      fetchImpl: (() => {
        n++;
        return Promise.resolve(new Response('{}', { status }));
      }) as unknown as typeof fetch,
      sleep: () => Promise.resolve(),
    });
    c.start();
    await until(() => links.includes(link));
    await tick();
    expect(n).toBe(1);
  });

  it('keeps trying through a controller that is down, and through an event it cannot read', async () => {
    let n = 0;
    const s = stream();
    const got: number[] = [];
    const c = new Connection({
      sessionId: 'ast_1',
      after: () => 0,
      onEvent: (e) => got.push(e.seq),
      onLink: () => {},
      fetchImpl: (() => {
        n++;
        if (n < 3) return Promise.reject(new TypeError('Failed to fetch'));
        return Promise.resolve(s.response);
      }) as unknown as typeof fetch,
      sleep: () => Promise.resolve(),
    });
    c.start();
    await until(() => n === 3);
    s.push('id: 1\nevent: delta\ndata: {not json\n\n' + frame({ seq: 2, type: 'delta', text: 'x' }));
    await until(() => got.length === 1);
    c.stop();
    expect(got).toEqual([2]);
  });

  it('gives up on a connection that has gone silent for too long and makes another', async () => {
    const quiet = stream();
    const next = stream();
    const queue = [quiet, next];
    let n = 0;
    const links: Link[] = [];
    const c = new Connection({
      sessionId: 'ast_1',
      after: () => 0,
      onEvent: () => {},
      onLink: (l) => links.push(l),
      staleMs: 30,
      fetchImpl: ((_u: string, init: RequestInit) => {
        n++;
        const r = queue.shift()!;
        init.signal?.addEventListener('abort', () => r.fail(new DOMException('aborted', 'AbortError'))); // what a real fetch does to its body
        return Promise.resolve(r.response);
      }) as unknown as typeof fetch,
      sleep: () => Promise.resolve(),
    });
    c.start();
    await until(() => n === 2);
    c.stop();
  });

  it('can be nudged to reconnect at once (the tab came back to the front)', async () => {
    const a = stream();
    const b = stream();
    const queue = [a, b];
    let n = 0;
    const c = new Connection({
      sessionId: 'ast_1',
      after: () => 0,
      onEvent: () => {},
      onLink: () => {},
      fetchImpl: ((_u: string, init: RequestInit) => {
        n++;
        const r = queue.shift()!;
        init.signal?.addEventListener('abort', () => r.fail(new DOMException('aborted', 'AbortError')));
        return Promise.resolve(r.response);
      }) as unknown as typeof fetch,
      sleep: () => Promise.resolve(),
    });
    c.start();
    await until(() => n === 1);
    c.nudge();
    await until(() => n === 2);
    c.stop();
  });
});
