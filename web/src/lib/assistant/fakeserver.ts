// A stand-in for the controller's assistant API, for tests of the page's side of it. It speaks the same routes and the same
// event stream (internal/api/assistant.go), and lets a test decide what the "provider" says and when the connection drops.

import type { AssistantEvent, AttachmentInfo, PendingAction, ProviderInfo, SessionView } from './types';

export const provider = (id: string, name: string, over: Partial<ProviderInfo> = {}): ProviderInfo => ({
  id, name, installed: true, signedIn: true, available: true,
  capabilities: { streaming: 'tokens', resume: true, cancel: true, modelChoice: true, reasoningChoice: true, voice: 'none' },
  models: {
    providerId: id, custom: true, reasoning: ['low', 'high'],
    models: id === 'codex' ? [{ id: 'gpt-a', name: 'GPT A', default: true, reasoning: ['low', 'medium'] }] : [{ id: 'sonnet', name: 'Sonnet' }, { id: 'opus', name: 'Opus' }],
  },
  ...over,
});

export interface Call {
  method: string;
  path: string;
  body?: unknown;
  headers: Headers;
  rawBody?: string;
}

interface Stream {
  push(e: Partial<AssistantEvent> & { type: AssistantEvent['type'] }): void;
  drop(): void;
}

export class FakeServer {
  providers: ProviderInfo[] = [provider('claude-code', 'Claude Code'), provider('codex', 'Codex')];
  policy = { enabled: true, maxBytes: 262144, maxFiles: 4, maxBytesTotal: 524288, retentionSeconds: 1800 };
  sessions = new Map<string, SessionView>();
  calls: Call[] = [];
  /** Status to answer a route with instead of its normal answer, by "METHOD path-prefix". */
  fail = new Map<string, number>();
  seq = 0;
  /** The turn the last message started: what events carry unless a test says otherwise. */
  turn = '';
  attachments: AttachmentInfo[] = [];
  private streams: Stream[] = [];
  private history: AssistantEvent[] = [];
  private nextId = 1;
  private waiters: (() => void)[] = [];
  /** Where the next stream begins counting; a restarted server begins again at 0. */
  lastEvent = 0;
  private enc = new TextEncoder();

  session(id: string): SessionView | undefined {
    return this.sessions.get(id);
  }

  /** Publishes an event to everyone listening, as the engine does. */
  emit(sessionId: string, e: Partial<AssistantEvent> & { type: AssistantEvent['type'] }): AssistantEvent {
    const full: AssistantEvent = { seq: ++this.lastEvent, sessionId, turnId: this.turn, at: '2026-03-09T09:00:00Z', ...e };
    const s = this.sessions.get(sessionId);
    if (s) s.lastEvent = this.lastEvent;
    this.history.push(full);
    for (const st of this.streams) st.push(full);
    this.notify();
    return full;
  }

  /** The controller was restarted: it remembers its conversations, but not its events, and counts from the start again. */
  restart(): void {
    this.dropStreams();
    this.history = [];
    this.lastEvent = 0;
    for (const s of this.sessions.values()) {
      s.running = false;
      s.lastEvent = 0;
    }
  }

  dropStreams(): void {
    for (const s of this.streams) s.drop();
    this.streams = [];
  }
  get streamCount(): number {
    return this.streams.length;
  }

  private notify() {
    for (const w of this.waiters.splice(0)) w();
  }
  /** Waits until the test's condition holds, giving the page's promises and timers room to run. */
  async until(f: () => boolean, what = 'condition'): Promise<void> {
    for (let i = 0; i < 1500; i++) {
      if (f()) return;
      await new Promise((r) => setTimeout(r, 2));
    }
    throw new Error('timed out waiting for ' + what);
  }

  called(method: string, pathPart: string): Call[] {
    return this.calls.filter((c) => c.method === method && c.path.includes(pathPart));
  }

  fetch = async (input: string | URL | Request, init: RequestInit = {}): Promise<Response> => {
    const url = new URL(String(input), 'http://localhost');
    const method = (init.method ?? 'GET').toUpperCase();
    const headers = new Headers(init.headers);
    let body: unknown;
    let rawBody: string | undefined;
    if (typeof init.body === 'string') {
      rawBody = init.body;
      try {
        body = JSON.parse(init.body);
      } catch {
        /* not JSON */
      }
    } else if (init.body instanceof Blob) rawBody = await init.body.text();
    const call: Call = { method, path: url.pathname, body, headers, rawBody };
    this.calls.push(call);
    const forced = [...this.fail].find(([k]) => k === `${method} ${url.pathname}` || (k.endsWith('*') && `${method} ${url.pathname}`.startsWith(k.slice(0, -1))));
    if (forced) return this.err(forced[1], 'forced', 'forced failure');

    const p = url.pathname.replace('/api/assistant', '');
    if (method === 'GET' && p === '/providers') return this.json(200, { providers: this.providers, attachments: this.policy });
    if (method === 'POST' && p === '/sessions') {
      const b = body as { provider: string; model?: string; reasoning?: string };
      const id = 'ast_' + this.nextId++;
      const v: SessionView = { id, provider: b.provider, model: b.model, reasoning: b.reasoning, state: 'idle', turns: 0, running: false, lastEvent: this.lastEvent, pending: [], attachments: [] };
      this.sessions.set(id, v);
      return this.json(201, v);
    }
    const m = /^\/sessions\/([^/]+)(?:\/([a-z]+)(?:\/([^/]+))?)?$/.exec(p);
    if (!m) return this.err(404, 'not_found', 'no such route');
    const [, id, sub, extra] = m;
    const s = this.sessions.get(id);
    if (!s) return this.err(404, 'not_found', 'not found');
    s.attachments = this.attachments.filter(() => true);
    if (!sub && method === 'GET') return this.json(200, { ...s, lastEvent: this.lastEvent });
    if (!sub && method === 'DELETE') {
      this.sessions.delete(id);
      return new Response(null, { status: 204 });
    }
    if (!sub && method === 'PATCH') {
      const b = body as { provider?: string; model?: string; reasoning?: string };
      if (b.provider && b.provider !== s.provider) {
        s.provider = b.provider;
        this.attachments = [];
        s.attachments = [];
      }
      if (b.model !== undefined) s.model = b.model;
      if (b.reasoning !== undefined) s.reasoning = b.reasoning;
      return this.json(200, s);
    }
    if (sub === 'messages' && method === 'POST') {
      if (s.running) return this.err(409, 'conflict', 'the assistant is still replying to the last message');
      s.running = true;
      const used = (body as { attachments?: string[] }).attachments ?? [];
      this.attachments = this.attachments.filter((a) => !used.includes(a.id));
      this.turn = 'trn_' + this.nextId++;
      return this.json(202, { turnId: this.turn });
    }
    if (sub === 'cancel' && method === 'POST') return this.json(200, { cancelled: s.running });
    if (sub === 'actions' && method === 'POST') return this.json(200, { ok: true, extra });
    if (sub === 'attachments' && method === 'POST') {
      if (headers.get('X-Attachment-Authorized') !== 'true') return this.err(400, 'invalid', 'a file is attached only with the person’s explicit authorization');
      const info: AttachmentInfo = { id: 'att_' + this.nextId++, name: decodeURIComponent(headers.get('X-Attachment-Name') ?? ''), bytes: rawBody?.length ?? 0, expiresAt: new Date(Date.now() + 1800_000).toISOString() };
      this.attachments.push(info);
      return this.json(201, info);
    }
    if (sub === 'attachments' && method === 'DELETE') {
      const before = this.attachments.length;
      this.attachments = this.attachments.filter((a) => a.id !== extra);
      return this.attachments.length < before ? new Response(null, { status: 204 }) : this.err(404, 'not_found', 'not waiting here');
    }
    if (sub === 'events' && method === 'GET') {
      let ctl!: ReadableStreamDefaultController<Uint8Array>;
      const stream = new ReadableStream<Uint8Array>({ start: (c) => (ctl = c) });
      const after = Number(headers.get('Last-Event-ID') ?? 0);
      const st: Stream = {
        // Like the engine: what happened before the client's last event is not replayed, but everything from now on is delivered.
        push: (e) => {
          const full = e as AssistantEvent;
          try {
            ctl.enqueue(this.enc.encode(`id: ${full.seq}\nevent: ${full.type}\ndata: ${JSON.stringify(full)}\n\n`));
          } catch {
            /* closed */
          }
        },
        drop: () => {
          try {
            ctl.close();
          } catch {
            /* already closed */
          }
        },
      };
      init.signal?.addEventListener('abort', () => {
        try {
          ctl.error(new DOMException('aborted', 'AbortError'));
        } catch {
          /* closed */
        }
        this.streams = this.streams.filter((x) => x !== st);
      });
      for (const e of this.history) if (e.seq > after) st.push(e);
      this.streams.push(st);
      this.notify();
      return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
    }
    return this.err(404, 'not_found', 'no such route');
  };

  /** A change waiting for the person, as the session lists it. */
  propose(sessionId: string, p: Partial<PendingAction> = {}): PendingAction {
    const a: PendingAction = { id: 'act_' + this.nextId++, operation: 'create_ticket', summary: 'Create task "Fix login"', expiresAt: new Date(Date.now() + 900_000).toISOString(), ...p };
    this.sessions.get(sessionId)?.pending.push(a);
    return a;
  }

  private json(status: number, body: unknown): Response {
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
  }
  private err(status: number, code: string, message: string): Response {
    return this.json(status, { error: { code, message } });
  }
}
