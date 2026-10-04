// What the interface says about execution policies. No framework code here, so
// it can be tested on its own.

import type { ExecutionPolicy, InteractionPolicy, Run, Task } from './types';

export interface InteractionOption {
  value: InteractionPolicy;
  /** What the picker says. */
  label: string;
  /** A short form for a card or a line of text. */
  short: string;
  /** One sentence on what it means, shown under the choice. */
  description: string;
}

/** In the order they are offered. The first is the default. */
export const INTERACTION_OPTIONS: readonly InteractionOption[] = [
  {
    value: 'interactive',
    label: 'Ask me when needed',
    short: 'Interactive',
    description: 'The agent may stop and ask you. The run waits for your answer, then carries on.',
  },
  {
    value: 'autonomous',
    label: 'Work autonomously',
    short: 'Autonomous',
    description: 'The agent investigates and decides for itself, and keeps going until it thinks the task is done.',
  },
  {
    value: 'autonomous_stop_if_blocked',
    label: 'Work autonomously — stop if blocked',
    short: 'Autonomous, stops if blocked',
    description: 'As above, but a decision it cannot safely make stops the run, with the reason, instead of a guess.',
  },
];

export const DEFAULT_POLICY: ExecutionPolicy = { interaction: 'interactive' };

/** The note that goes with every choice: a policy changes who answers questions, never what the agent may do. */
export const PERMISSIONS_NOTE = 'This never widens what the agent may do: permission requests still come to you.';

export function isInteraction(v: string): v is InteractionPolicy {
  return INTERACTION_OPTIONS.some((o) => o.value === v);
}

function option(p: InteractionPolicy | undefined): InteractionOption {
  return INTERACTION_OPTIONS.find((o) => o.value === p) ?? INTERACTION_OPTIONS[0];
}

/** The picker's wording for a policy. A missing policy is the default. */
export function interactionLabel(p: ExecutionPolicy | undefined): string {
  return option(p?.interaction).label;
}

/** The short wording, for a card. */
export function interactionShort(p: ExecutionPolicy | undefined): string {
  return option(p?.interaction).short;
}

/** Whether a task or run deserves a mention on a card: the default does not. */
export function isNotable(p: ExecutionPolicy | undefined): boolean {
  return !!p && p.interaction !== 'interactive';
}

/** The policy a run will start with: the task's, unless the user chose another for it. */
export function policyForRun(task: Pick<Task, 'policy'> | undefined, chosen?: InteractionPolicy): ExecutionPolicy {
  return { interaction: chosen ?? task?.policy?.interaction ?? 'interactive' };
}

/** A line for a blocked run: what is in the way, in the user's terms. */
export function blockerLine(run: Pick<Run, 'blocker'>): string {
  const b = run.blocker;
  if (!b) return 'The agent stopped rather than guess.';
  return b.summary;
}
