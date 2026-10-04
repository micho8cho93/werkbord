// What the interface knows about questions: how they are named, which are still
// open, and what to tell the user when an answer does not go through. No
// framework code here, so it can be tested on its own.

import { ApiError } from './api';
import type { ControllerEvent, Question, QuestionKind } from './types';

/** A short tag that says what the agent wants, in the user's terms. */
export function kindLabel(kind: QuestionKind): string {
  switch (kind) {
    case 'approval':
      return 'Needs your approval';
    case 'decision':
      return 'Needs your decision';
    case 'selection':
      return 'Needs a choice';
    case 'instruction':
      return 'Needs instructions';
    case 'clarification':
      return 'Needs more information';
  }
}

/** The same, as a heading for a list: "Approval", "Decision". */
export function kindNoun(kind: QuestionKind): string {
  return { approval: 'Approval', decision: 'Decision', selection: 'Choice', instruction: 'Instructions', clarification: 'Question' }[kind];
}

/** Placeholder for the box where a typed answer goes. */
export function freeTextHint(q: Question): string {
  const hasChoices = (q.options?.length ?? 0) > 0;
  if (hasChoices) return 'Or write your own answer';
  switch (q.kind) {
    case 'instruction':
      return 'Tell the agent what to do next';
    case 'decision':
      return 'Your decision';
    default:
      return 'Your answer';
  }
}

/** Whether a choice grants something (so it is drawn as the main action) or refuses it. */
export const isAllow = (o: string) => /^(allow|yes|approve|accept|proceed|continue)\b/i.test(o);
export const isDeny = (o: string) => /^(deny|no|decline|reject|cancel|stop)\b/i.test(o);

/** The choices to draw for a question: its options, or Allow/Deny for an approval without any. */
export function choicesOf(q: Question): string[] {
  if (q.options?.length) return q.options;
  return q.kind === 'approval' ? ['Allow', 'Deny'] : [];
}

/** Whether the question takes a typed answer. A question without choices always does. */
export function takesText(q: Question): boolean {
  return q.allowFreeText || choicesOf(q).length === 0;
}

/** Context is shown folded away when it is long, so it does not push the choices off a phone screen. */
export function contextIsLong(context: string): boolean {
  return context.length > 360 || context.split('\n').length > 7;
}

/** "Open to read the full command" style label for folded context. */
export function contextSummary(context: string): string {
  const lines = context.trim().split('\n').length;
  return lines > 1 ? `Details (${lines} lines)` : 'Details';
}

/** Says how a question the controller answered for the user, as the run's policy said, was dealt with. */
export function answerLine(q: Question): string {
  return q.answeredBy === 'policy'
    ? 'You were not asked: this run does not put routine questions to you, so Devboard told the agent to settle it itself.'
    : `You answered: ${q.answer ?? ''}`;
}

/** Says in a sentence why a question was closed. */
export function closedText(q: Question): string {
  const kept = q.answer ? ' Your answer was not delivered.' : '';
  switch (q.cancelReason) {
    case 'run_ended':
      return `The agent stopped before this was answered.${kept}`;
    case 'interrupted':
      return `The session was interrupted before this was answered. Send a message to continue it.${kept}`;
    case 'withdrawn':
      return `The agent no longer needs an answer.${kept}`;
    default:
      return `No longer needed.${kept}`;
  }
}

/** What the user is told when answering did not go through, and whether to drop the question. */
export interface AnswerFailure {
  message: string;
  /** The question is settled: stop offering it. */
  settled: boolean;
  /** Show as a notice that outlives the card, rather than inside it. */
  notice: boolean;
}

export function describeAnswerFailure(err: unknown): AnswerFailure {
  if (err instanceof ApiError) {
    const q = err.question;
    if (q && err.code === 'question_answered') {
      return { message: `Already answered${q.answer ? `: “${q.answer}”` : ''}. Nothing was sent again.`, settled: true, notice: true };
    }
    if (q && err.code === 'question_closed') {
      return { message: closedText(q), settled: true, notice: true };
    }
    if (err.code === 'offline') {
      return { message: 'Cannot reach the controller. Your answer was not sent; it is safe to try again.', settled: false, notice: false };
    }
    if (err.code === 'not_found') {
      return { message: 'This question no longer exists.', settled: true, notice: true };
    }
    return { message: err.message, settled: false, notice: false };
  }
  return { message: err instanceof Error ? err.message : String(err), settled: false, notice: false };
}

/** The page title, which shows on a tab or a notification badge even when the app is in the background. */
export function pageTitle(pending: number, base = 'Devboard'): string {
  return pending > 0 ? `(${pending}) Needs input · ${base}` : base;
}

/** "1 question needs you", "3 questions need you". */
export function needsInputText(n: number): string {
  return n === 1 ? '1 question needs your input' : `${n} questions need your input`;
}

/**
 * The questions that are still open, kept current from two sources that can
 * disagree about timing: lists fetched over HTTP and events from the stream.
 *
 * Whatever arrives, a question that was seen closed is never shown again, and a
 * question that was asked while a list was in flight is not lost when the list
 * (taken a moment earlier) arrives without it.
 */
export class QuestionBook {
  private open = new Map<string, Question>();
  private closed = new Set<string>();
  private syncId = 0;
  private asked: Map<string, Question> | null = null;

  /** The open questions, oldest first: the one that has waited longest comes first. */
  get pending(): Question[] {
    return [...this.open.values()].sort((a, b) => a.askedAt.localeCompare(b.askedAt) || a.id.localeCompare(b.id));
  }

  /** Call before fetching the list; pass the result to `replace`. */
  beginSync(): number {
    this.asked = new Map();
    return ++this.syncId;
  }

  /** Takes a fetched list as the truth, unless a newer fetch has started since. Returns whether it was used. */
  replace(syncId: number, list: Question[]): boolean {
    if (syncId !== this.syncId) return false;
    const next = new Map<string, Question>();
    for (const q of list) if (q.state === 'pending' && !this.closed.has(q.id)) next.set(q.id, q);
    for (const q of this.asked?.values() ?? []) if (!this.closed.has(q.id)) next.set(q.id, q);
    this.open = next;
    this.asked = null;
    return true;
  }

  /** A question was asked. */
  ask(q: Question): void {
    if (q.state !== 'pending' || this.closed.has(q.id)) return;
    this.open.set(q.id, q);
    this.asked?.set(q.id, q);
  }

  /** A question was answered or cancelled: it is no longer open, whoever closed it. */
  resolve(q: Pick<Question, 'id'>): void {
    this.closed.add(q.id);
    this.open.delete(q.id);
    this.asked?.delete(q.id);
  }

  /** Applies an event from the stream. Returns whether the open questions changed. */
  apply(ev: ControllerEvent): boolean {
    const q = (ev.payload as { question?: Question } | undefined)?.question;
    if (!q) return false;
    const before = this.open.size;
    switch (ev.type) {
      case 'agent.question':
        this.ask(q);
        break;
      case 'question.answered':
      case 'question.cancelled':
        this.resolve(q);
        break;
      default:
        return false;
    }
    return this.open.size !== before || ev.type === 'agent.question';
  }
}
