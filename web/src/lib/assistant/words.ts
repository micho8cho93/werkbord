// The words the screen uses where the system speaks in codes. Everything the person reads about what went wrong is decided
// here, in one place, so that it is the same everywhere and can be tested.

import { ApiError } from '../api';
import type { ErrorInfo } from './types';

/** What the person is told when a request to the assistant fails. */
export function explain(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return 'Cannot reach Werkbord on this computer. Check the connection before sending the request again.';
    if (e.status === 401) return 'Werkbord could not authorise this request. Open it again from the Werkbord link.';
    if (e.status === 503) return 'Werkbord could not handle this Salie request. Check the conversation before repeating it.';
    if (e.status === 413) return e.message;
    return e.message || 'Werkbord could not complete this request.';
  }
  return e instanceof Error ? e.message : 'Werkbord could not complete this request.';
}

export type FailureAction = 'retry' | 'model' | 'recheck' | 'none';

export interface Failure {
  title: string;
  detail: string;
  action: FailureAction;
}

/** A failed turn, in words and with the one thing most worth offering. */
export function failure(err: ErrorInfo): Failure {
  const detail = err.message;
  switch (err.code) {
    case 'model_unavailable':
      return { title: 'That model is not available to your account', detail, action: 'model' };
    case 'not_signed_in':
    case 'not_installed':
      return { title: err.code === 'not_installed' ? 'The coding agent is not installed' : 'You are signed out of the coding agent', detail, action: 'recheck' };
    case 'rate_limited':
      return { title: 'The coding agent’s service is limiting requests', detail, action: 'none' };
    case 'timeout':
      return { title: 'Salie took too long to finish this reply', detail, action: 'retry' };
    case 'too_many_steps':
      return { title: 'Salie went round in circles', detail, action: 'none' };
    case 'policy':
      return { title: 'Salie stopped for safety', detail, action: 'none' };
    case 'protocol':
      return { title: 'Werkbord could not read Salie’s response', detail, action: 'none' };
    case 'audit_unavailable':
      return { title: 'Werkbord could not write its records', detail, action: err.retryable ? 'retry' : 'none' };
    default:
      return { title: 'Salie could not finish this reply', detail, action: err.retryable ? 'retry' : 'none' };
  }
}

export function size(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.ceil(bytes / 1024))} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** "30 minutes", "1 hour", "2 hours". */
export function duration(seconds: number): string {
  const m = Math.round(seconds / 60);
  if (m < 60) return m === 1 ? '1 minute' : `${m} minutes`;
  const h = Math.round(m / 60);
  return h === 1 ? '1 hour' : `${h} hours`;
}

/** What the retention rule says to someone about to share a file. */
export function retentionNote(seconds: number): string {
  return `Kept on this computer for up to ${duration(seconds)}, or until you send or remove it. Never written to disk.`;
}

/** The part of a time of day a person wants: "14:32". */
export function clock(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** What a proposed change's operation is, in plain words. */
export function operationTitle(op: string): string {
  switch (op) {
    case 'create_ticket':
      return 'New task';
    case 'update_ticket':
      return 'Change a task';
    case 'answer_question':
      return 'Answer an agent’s question';
    case 'start_agent_run':
      return 'Start an agent';
    default:
      return 'Change';
  }
}

/** Names the failed assistant operation without exposing its compatibility tool identifier. */
export function operationFailureTitle(op: string): string {
  switch (op) {
    case 'list_projects': return 'Could not load projects';
    case 'list_tickets': return 'Could not load tasks';
    case 'get_ticket': return 'Could not load this task';
    case 'get_schedule': return 'Could not load the schedule';
    case 'list_blockers': return 'Could not load blockers';
    case 'list_agent_questions': return 'Could not load agent questions';
    case 'list_runs': return 'Could not load runs';
    case 'get_run_status': return 'Could not load run status';
    case 'create_ticket': return 'Could not create the task';
    case 'update_ticket': return 'Could not update the task';
    case 'answer_question': return 'Could not save the answer';
    case 'start_agent_run': return 'Could not start the agent';
    default: return 'Could not complete this Salie step';
  }
}

/** Whether a file's first bytes look like text. The server decides for real; this saves sending what will be refused. */
export function looksLikeText(sample: Uint8Array): boolean {
  for (const b of sample) {
    if (b === 0) return false;
    if (b < 32 && b !== 9 && b !== 10 && b !== 12 && b !== 13) return false;
  }
  return true;
}
