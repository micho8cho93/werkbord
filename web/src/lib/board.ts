// What the board does and says, without the framework, so it can be tested on its own.

import { cardActivity, oneLine, runElapsed, runStatus, type Tone } from './format';
import type { Question, Run, SchedulingDecision, Task, TaskState } from './types';
import { schedulingLabels } from './scheduling';
import { modeOf } from './labels';
import type { Project } from './types';

/** Whether an agent may be started on the task: not human work, and not in a project with no repository. */
export function agentWorkable(task: Pick<Task, 'workMode'>, project: Pick<Project, 'kind'> | undefined): boolean {
  return project?.kind !== 'work' && modeOf(task) !== 'human';
}

/**
 * What dropping a card on a column does. The board follows the rules of the work, not of the
 * furniture:
 *
 *   - Backlog → Doing starts an agent on the task (the controller then puts it in Doing). A
 *     task whose agent is already working just moves.
 *   - Review → Done is a merge: the merge is confirmed first, and the card moves once it is
 *     done. Work without a branch to merge simply moves.
 *   - Anything else moves the card, as choosing its column does.
 */
export type DropAction = 'start' | 'merge' | 'move' | 'none';

export function dropAction(task: Task, run: Run | undefined, to: TaskState, mergeableBranch: boolean, agentWork = true): DropAction {
  if (to === task.state) return 'none';
  const active = !!run && runStatus(run).active;
  // Work no agent can do (human work, or a project with no repository) only moves: dropping it on Doing
  // means a person has started it.
  if (agentWork && task.state === 'backlog' && to === 'doing' && !active) return 'start';
  if (task.state === 'review' && to === 'done' && mergeableBranch && !active) return 'merge';
  return 'move';
}

export interface CardStatus {
  tone: Tone;
  /** The status line: "Running · 12m · this computer". */
  text: string;
  /** What is happening or wanted, in one line of machine text. Empty if nothing to say. */
  detail: string;
  /** The agent is at work right now: the card is drawn live. */
  live: boolean;
}

export interface CardContext {
  now: number;
  /** The runner's name, when it is worth saying (more than one machine). */
  runnerName?: string;
  questions: readonly Question[];
  decision?: SchedulingDecision;
  /** Titles of unfinished tasks this one waits for. */
  waitingFor: readonly string[];
  /** The task's branch as Git sees it ("werkbord/fix-auth · 3 commits ahead"), when known. */
  branch?: string;
}

export function cardStatus(task: Task, run: Run | undefined, ctx: CardContext): CardStatus {
  if (run) {
    const s = runStatus(run);
    const where = ctx.runnerName ? ` · ${ctx.runnerName}` : '';
    if (s.active) {
      const elapsed = runElapsed(run, ctx.now);
      const q = ctx.questions[0];
      if (q) {
        const more = ctx.questions.length > 1 ? ` (+${ctx.questions.length - 1} more)` : '';
        return { tone: 'ask', text: `Needs you · ${sinceShort(q.askedAt, ctx.now)}${more}`, detail: oneLine(q.prompt, 180), live: false };
      }
      if (run.state === 'blocked') return { tone: 'block', text: 'Blocked · stopped rather than guess', detail: oneLine(cardActivity(run), 180), live: false };
      if (s.tone === 'idle') return { tone: 'idle', text: `Waiting for your reply · ${elapsed}`, detail: oneLine(run.activity ?? '', 180), live: false };
      return { tone: 'work', text: `${s.label} · ${elapsed}${where}`, detail: run.activity ? `› ${oneLine(run.activity, 180)}` : '', live: true };
    }
    if (task.state === 'review' && run.state === 'completed') return { tone: 'ok', text: 'Ready for review', detail: ctx.branch ?? run.branch ?? '', live: false };
    if (task.state === 'done') return { tone: 'ok', text: `Done · ${sinceShort(task.updatedAt, ctx.now)} ago`, detail: '', live: false };
    if (run.state === 'failed') return { tone: 'bad', text: `Failed · ${sinceShort(run.endedAt ?? run.updatedAt, ctx.now)} ago`, detail: oneLine(run.reason ?? '', 180), live: false };
    if (run.state === 'stopped') return { tone: 'neutral', text: 'Stopped', detail: oneLine(run.reason ?? '', 180), live: false };
    return { tone: 'ok', text: 'Session finished · ready to review', detail: ctx.branch ?? run.branch ?? '', live: false };
  }
  if (task.state === 'done') return { tone: 'ok', text: `Done · ${sinceShort(task.updatedAt, ctx.now)} ago`, detail: '', live: false };
  if (ctx.waitingFor.length) return { tone: 'neutral', text: `Waits for ${ctx.waitingFor.join(', ')}`, detail: '', live: false };
  if (ctx.decision && task.orchestration?.enabled && !task.orchestration.runId) {
    return { tone: 'neutral', text: schedulingLabels[ctx.decision.state], detail: oneLine(ctx.decision.reason, 140), live: false };
  }
  return { tone: 'neutral', text: 'Not started', detail: '', live: false };
}

/** "4m", "3h", "2d": how long ago, said briefly. */
export function sinceShort(iso: string, now: number): string {
  const secs = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}

/** Choices short enough to answer from the card itself; longer or typed answers open the task. */
export function quickChoices(choices: readonly string[]): string[] {
  if (choices.length === 0 || choices.length > 3) return [];
  return choices.every((c) => c.length <= 24) ? [...choices] : [];
}
