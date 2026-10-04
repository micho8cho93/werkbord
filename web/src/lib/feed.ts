// Turns the stream of a run's events into what the task page shows: messages,
// grouped tool use, questions and a few lifecycle markers. This is deliberately
// not a terminal: the point is to show what happened and what needs doing.
// No framework code here, so it can be tested on its own.

import type { AgentOutput, Blocker, ControllerEvent, Question } from './types';

export interface ToolLine {
  seq: number;
  text: string;
}

export type FeedItem =
  | { kind: 'message'; seq: number; role: 'assistant' | 'user'; text: string; at: string }
  | { kind: 'tools'; seq: number; lines: ToolLine[] }
  | { kind: 'notice'; seq: number; text: string }
  | { kind: 'diagnostics'; seq: number; lines: ToolLine[] }
  | { kind: 'marker'; seq: number; tone: 'ok' | 'bad' | 'neutral' | 'info' | 'block'; text: string; detail?: string; at: string }
  | { kind: 'question'; seq: number; question: Question; at: string };

/** The line an agent ends its turn with when it is blocked; see agent.BlockerMarker in the controller. */
const BLOCKER_MARKER = 'DEVBOARD_BLOCKED';

/** Text with any blocker-report lines taken out. */
export function withoutBlockerReport(text: string): string {
  if (!text.includes(BLOCKER_MARKER)) return text;
  return text
    .split('\n')
    .filter((line) => !line.trim().startsWith(BLOCKER_MARKER))
    .join('\n')
    .trim();
}

/**
 * Builds a feed incrementally. Events may be applied more than once or out of
 * order (a reconnect replays some); each is applied once, and the feed always
 * reads in sequence order.
 */
export class FeedBuilder {
  /**
   * The feed. Items are never changed in place: a change replaces the item and
   * the array, so a view that keys on identity redraws only what changed.
   */
  items: FeedItem[] = [];
  private seen = new Set<number>();
  /** Highest sequence applied; where a reconnect should resume. */
  lastSeq = 0;
  /** Lowest sequence applied; where older history would end. */
  firstSeq = 0;

  /** Applies events in any order. Returns true if the feed changed. */
  apply(events: ControllerEvent[]): boolean {
    const fresh = events.filter((e) => !this.seen.has(e.seq)).sort((a, b) => a.seq - b.seq);
    if (fresh.length === 0) return false;
    // Anything that predates what is shown means rebuilding, so order stays right.
    if (this.lastSeq > 0 && fresh[0].seq < this.lastSeq) {
      const all = this.history.concat(fresh).sort((a, b) => a.seq - b.seq);
      this.reset();
      for (const e of all) this.append(e);
      return true;
    }
    this.items = this.items.slice();
    for (const e of fresh) this.append(e);
    return true;
  }

  private history: ControllerEvent[] = [];
  /** Whether the run is blocked at the point of the feed being built, to tell what ends it. */
  private blocked = false;

  private reset() {
    this.blocked = false;
    this.items = [];
    this.seen = new Set();
    this.history = [];
    this.lastSeq = 0;
    this.firstSeq = 0;
  }

  private append(ev: ControllerEvent) {
    this.seen.add(ev.seq);
    this.history.push(ev);
    this.lastSeq = Math.max(this.lastSeq, ev.seq);
    this.firstSeq = this.firstSeq === 0 ? ev.seq : Math.min(this.firstSeq, ev.seq);

    switch (ev.type) {
      case 'agent.output':
        this.output(ev, ev.payload as AgentOutput);
        break;
      case 'agent.question': {
        const q = (ev.payload as { question: Question }).question;
        this.items.push({ kind: 'question', seq: ev.seq, question: q, at: ev.createdAt });
        break;
      }
      case 'question.answered':
      case 'question.cancelled': {
        // Either way the question is closed; the item shows what became of it.
        const q = (ev.payload as { question: Question }).question;
        this.items = this.items.map((item) => (item.kind === 'question' && item.question.id === q.id ? { ...item, question: q } : item));
        break;
      }
      case 'agent.started': {
        const resumed = (ev.payload as { resumed?: boolean }).resumed;
        this.items.push({ kind: 'marker', seq: ev.seq, tone: 'info', text: resumed ? 'Session resumed' : 'Agent started', at: ev.createdAt });
        break;
      }
      case 'agent.waiting': {
        const p = ev.payload as { reason?: string; detail?: string };
        if (p.reason === 'interrupted') {
          this.items.push({ kind: 'marker', seq: ev.seq, tone: 'neutral', text: 'Session interrupted', detail: 'Send a message to continue the conversation.', at: ev.createdAt });
        }
        break;
      }
      case 'agent.blocked': {
        this.blocked = true;
        const b = (ev.payload as { blocker?: Blocker }).blocker;
        this.items.push({
          kind: 'marker',
          seq: ev.seq,
          tone: 'block',
          text: 'Blocked: the agent stopped rather than guess',
          detail: b?.summary,
          at: ev.createdAt,
        });
        break;
      }
      case 'agent.resumed': {
        // Leaving the blocked state is worth saying; ordinary resumes are visible as the message that caused them.
        const via = (ev.payload as { via?: string }).via;
        if (via === 'message' && this.blocked) {
          this.items.push({ kind: 'marker', seq: ev.seq, tone: 'info', text: 'Unblocked by your message', at: ev.createdAt });
        }
        this.blocked = false;
        break;
      }
      case 'agent.completed':
        this.items.push({ kind: 'marker', seq: ev.seq, tone: 'ok', text: 'Session finished', at: ev.createdAt });
        break;
      case 'agent.failed': {
        const p = ev.payload as { reason?: string };
        this.items.push({ kind: 'marker', seq: ev.seq, tone: 'bad', text: 'Failed', detail: p.reason, at: ev.createdAt });
        break;
      }
      case 'agent.stopped': {
        const p = ev.payload as { reason?: string };
        this.items.push({ kind: 'marker', seq: ev.seq, tone: 'neutral', text: 'Stopped', detail: p.reason, at: ev.createdAt });
        break;
      }
    }
  }

  private output(ev: ControllerEvent, out: AgentOutput) {
    let text = out.text ?? '';
    // A blocker report is for the controller: the feed shows it as the "Blocked" marker, not as raw text.
    if (out.stream === 'assistant') text = withoutBlockerReport(text);
    if (text.trim() === '') return;
    const last = this.items[this.items.length - 1];
    switch (out.stream) {
      case 'assistant':
      case 'user':
        this.items.push({ kind: 'message', seq: ev.seq, role: out.stream, text, at: ev.createdAt });
        break;
      case 'tool':
        // Runs of tool use are one item: what matters is that the agent is busy, and with what last.
        if (last?.kind === 'tools') this.items[this.items.length - 1] = { ...last, lines: [...last.lines, { seq: ev.seq, text }] };
        else this.items.push({ kind: 'tools', seq: ev.seq, lines: [{ seq: ev.seq, text }] });
        break;
      case 'stderr':
        if (last?.kind === 'diagnostics') this.items[this.items.length - 1] = { ...last, lines: [...last.lines, { seq: ev.seq, text }] };
        else this.items.push({ kind: 'diagnostics', seq: ev.seq, lines: [{ seq: ev.seq, text }] });
        break;
      default:
        this.items.push({ kind: 'notice', seq: ev.seq, text });
    }
  }
}
