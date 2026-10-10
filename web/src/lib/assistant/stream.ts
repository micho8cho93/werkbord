// The conversation's event stream, read with fetch so the credential can travel in a header. A turn belongs to the engine, not
// to the request that started it, so everything here is about getting back in step: reconnect with the last event seen, drop
// what has been seen, and say plainly what state the connection is in.

import { getToken } from '../api';
import type { AssistantEvent } from './types';

export interface SSEMessage {
  id?: string;
  event: string;
  data: string;
}

/** An incremental parser for text/event-stream. Chunks may end anywhere, including between a CR and its LF. */
export class SSEParser {
  private buf = '';
  private lastCR = false;
  private id: string | undefined;
  private event = '';
  private data: string[] = [];

  feed(chunk: string): SSEMessage[] {
    const out: SSEMessage[] = [];
    // A line ending split across chunks (CR, then LF) is one ending, not two.
    if (this.lastCR && chunk.startsWith('\n')) chunk = chunk.slice(1);
    if (chunk === '') return out;
    this.lastCR = chunk.endsWith('\r');
    this.buf += chunk;
    let start = 0;
    let i = 0;
    while (i < this.buf.length) {
      const c = this.buf[i];
      if (c !== '\n' && c !== '\r') {
        i++;
        continue;
      }
      const line = this.buf.slice(start, i);
      if (c === '\r' && this.buf[i + 1] === '\n') i++;
      i++;
      start = i;
      const m = this.line(line);
      if (m) out.push(m);
    }
    this.buf = this.buf.slice(start);
    return out;
  }

  private line(line: string): SSEMessage | null {
    if (line === '') {
      const had = this.data.length > 0 || this.event !== '';
      const m: SSEMessage = { id: this.id, event: this.event || 'message', data: this.data.join('\n') };
      this.event = '';
      this.data = [];
      return had ? m : null;
    }
    if (line.startsWith(':')) return null; // a comment: the server's heartbeat
    const i = line.indexOf(':');
    const field = i < 0 ? line : line.slice(0, i);
    let value = i < 0 ? '' : line.slice(i + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    if (field === 'event') this.event = value;
    else if (field === 'data') this.data.push(value);
    else if (field === 'id' && !value.includes('\0')) this.id = value;
    return null;
  }
}

/** How the connection to the conversation is doing, in words a person could be told. */
export type Link = 'idle' | 'connecting' | 'live' | 'reconnecting' | 'unauthorized' | 'gone' | 'unavailable';

export interface ConnectionOptions {
  sessionId: string;
  /** The last event already applied; asked for again on every (re)connect. */
  after: () => number;
  onEvent: (e: AssistantEvent) => void;
  onLink: (l: Link) => void;
  /** Called after each connect succeeds, before events flow, to reconcile with the server (it may have restarted). */
  onOpen?: () => Promise<void> | void;
  fetchImpl?: typeof fetch;
  /** Wait between attempts; injectable for tests. */
  sleep?: (ms: number) => Promise<void>;
  /** No byte for this long means the connection is dead (the server pings every 25 s). */
  staleMs?: number;
}

export const backoff = (attempt: number): number => Math.min(500 * 2 ** Math.max(0, attempt - 1), 8000);

/** Keeps a conversation's event stream open until stopped. */
export class Connection {
  private ctl: AbortController | null = null;
  private stopped = true;
  private attempt = 0;

  constructor(private o: ConnectionOptions) {}

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.attempt = 0;
    void this.loop();
  }

  stop(): void {
    this.stopped = true;
    this.ctl?.abort();
    this.o.onLink('idle');
  }

  /** Drops the connection so it is made again at once (the page became visible, the network came back). */
  nudge(): void {
    if (!this.stopped) this.ctl?.abort();
  }

  private async loop(): Promise<void> {
    const sleep = this.o.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
    while (!this.stopped) {
      this.o.onLink(this.attempt === 0 ? 'connecting' : 'reconnecting');
      const verdict = await this.once();
      if (this.stopped || verdict === 'fatal') return;
      this.attempt++;
      await sleep(backoff(this.attempt));
    }
  }

  /** One connection, until it ends. 'fatal' means retrying cannot help. */
  private async once(): Promise<'retry' | 'fatal'> {
    const ctl = new AbortController();
    this.ctl = ctl;
    const f = this.o.fetchImpl ?? fetch;
    const stale = this.o.staleMs ?? 70_000;
    let watchdog: ReturnType<typeof setTimeout> | undefined;
    const arm = () => {
      clearTimeout(watchdog);
      watchdog = setTimeout(() => ctl.abort(), stale);
    };
    try {
      const headers: Record<string, string> = { Accept: 'text/event-stream' };
      const token = getToken();
      if (token) headers.Authorization = `Bearer ${token}`;
      const after = this.o.after();
      if (after > 0) headers['Last-Event-ID'] = String(after);
      arm();
      const res = await f(`/api/assistant/sessions/${encodeURIComponent(this.o.sessionId)}/events`, { headers, signal: ctl.signal, cache: 'no-store' });
      if (res.status === 401 || res.status === 403) {
        this.o.onLink('unauthorized');
        return 'fatal';
      }
      if (res.status === 404) {
        this.o.onLink('gone');
        return 'fatal';
      }
      if (res.status === 503) {
        this.o.onLink('unavailable');
        return 'fatal';
      }
      if (!res.ok || !res.body) return 'retry';
      await this.o.onOpen?.();
      if (this.stopped) return 'fatal';
      // onOpen has reconciled the event numbers with the server's (a restart counts from the start again) before any event is read.
      this.attempt = 0;
      this.o.onLink('live');
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      const parser = new SSEParser();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        arm();
        for (const m of parser.feed(dec.decode(value, { stream: true }))) {
          if (!m.data) continue;
          try {
            this.o.onEvent(JSON.parse(m.data) as AssistantEvent);
          } catch {
            // One event that cannot be read is skipped; the next one still counts.
          }
        }
      }
      return 'retry';
    } catch {
      return 'retry';
    } finally {
      clearTimeout(watchdog);
      if (this.ctl === ctl) this.ctl = null;
    }
  }
}
