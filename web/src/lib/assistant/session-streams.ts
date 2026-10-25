import { Connection, type ConnectionOptions, type StreamConnection } from './stream';
import type { AssistantEvent, SessionView } from './types';

/** Logical per-session connections share one authenticated HTTP stream. Each retains its own
 * cursor, callbacks and lifecycle. Switching/closing UI tabs never changes subscriptions. */
export class SessionStreams {
  private members = new Map<string, ConnectionOptions>();
  private transport: Connection | undefined;
  private queued = false;
  connect = (options: ConnectionOptions): StreamConnection => ({
    start: () => { this.members.set(options.sessionId, options); this.restart(); },
    stop: () => {
      if (this.members.get(options.sessionId) !== options) return;
      this.members.delete(options.sessionId); options.onLink('idle'); this.restart();
    },
    nudge: () => this.transport?.nudge(),
  });
  private restart(): void {
    if (this.queued) return;
    this.queued = true;
    queueMicrotask(() => {
      this.queued = false;
      this.transport?.stop(); this.transport = undefined;
      if (!this.members.size) return;
      this.transport = new Connection({
        sessionId: '', after: () => 0,
        request: () => ({ url: '/api/assistant/events', body: { sessions: [...this.members.values()].map(o => ({id:o.sessionId,after:o.after()})) } }),
        onLink: link => { if (link !== 'idle') for (const o of this.members.values()) o.onLink(link); },
        onEvent: event => {
          const o = this.members.get(event.sessionId);
          if (!o) return;
          const frame = event as unknown as {type:string; session?:SessionView};
          if (frame.type === 'session_snapshot') {
            if (frame.session?.id === event.sessionId) o.onSnapshot?.(frame.session);
          } else if (frame.type === 'session_missing') o.onLink('gone');
          else o.onEvent(event as AssistantEvent);
        },
      });
      this.transport.start();
    });
  }
}
