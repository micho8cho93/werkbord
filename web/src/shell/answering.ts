// Answering an agent's question from the window, for the Personal workspace: one path for every place that offers it (the decision panel on
// Work, the list of questions under it). It only relays what the person typed, as AnswerQuestion, to the workspace that asked. Whether that
// workspace can be reached now is decided from its last summary, so an answer is never offered to a workspace that has gone quiet.

import { ApiError } from '../lib/api';
import type { Question } from '../lib/types';
import { ask } from './native';
import { decisionKey } from './decisions';
import { decisions } from './decisions.svelte';
import { model } from './model.svelte';
import type { Overview } from './types';

/** How old a workspace's summary may be before its questions are no longer offered for answering. */
export const FRESH_FOR = 30_000;

/** Whether the Personal workspace has answered recently enough that an answer sent now would reach it. */
export function reachable(overview: Overview | null, now: number, id = 'personal'): boolean {
  const e = overview?.entries.find((x) => x.workspace.id === id);
  return !!e?.summary && !e.error && e.workspace.state === 'ready' && now - Date.parse(e.summary.at) < FRESH_FOR;
}

export async function answerWith(q: Question, answer: string, overview: Overview | null = model.overview, now = Date.now()): Promise<Question> {
  if (!reachable(overview, now)) throw new Error('Personal is offline or stale. Reconnect before answering.');
  const result = (await ask((a) => a.Relay('personal', 'AnswerQuestion', [{ projectId: q.projectId, questionId: q.id, answer }]))) as {
    status: number;
    body: Question & { error?: { code: string; message: string }; question?: Question };
  };
  if (result.status >= 400) throw new ApiError(result.status, result.body.error?.code ?? 'unknown', result.body.error?.message ?? 'Could not save the answer.', result.body.question);
  return result.body;
}

/** The workspace accepted the answer (or says it was settled elsewhere): the decision is out of the way at once, and the overview is read again. */
export function settled(q: Pick<Question, 'id'>): void {
  decisions.settle(decisionKey('personal', 'question-' + q.id));
  void model.refreshOverview();
}
