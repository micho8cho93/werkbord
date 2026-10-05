// How work is carried out, and what the interface says about it. No framework
// code here, so it can be tested on its own.
//
// The hierarchy is global defaults, then a project's, then a task's, and a run can
// differ for itself. The first level that sets a field wins. This mirrors
// domain.ResolveExecution in the controller, which is what actually decides when a
// run starts; this copy only lets the interface say, before the run, what it will
// get and where that comes from.

import type { Agent, AgentOption, AgentOptions, ExecutionConfig, InteractionPolicy, Priority, Runner } from './types';

/** The model or reasoning value that means "pass nothing: the agent's own default". */
export const AGENT_DEFAULT = 'default';

export type Source = 'run' | 'task' | 'project' | 'global' | 'default';

export interface Level {
  source: Source;
  config: ExecutionConfig;
}

export interface Resolved {
  runner: string;
  /** Empty when no level chose one: the controller then uses the first agent that can be used. */
  agent: string;
  /** Empty means the agent's default. */
  model: string;
  reasoning: string;
  interaction: InteractionPolicy;
  priority: Priority;
  sources: { agent: Source; model: Source; reasoning: Source; interaction: Source; priority: Source };
}

/**
 * Applies the hierarchy. `levels` are in order of precedence, highest first. The agent is
 * resolved first; a level's model or reasoning is only considered if that level's own agent
 * is the one that won, because a model name means nothing to another agent.
 */
export function resolveExecution(...levels: Level[]): Resolved {
  const r: Resolved = {
    runner: '',
    agent: '',
    model: '',
    reasoning: '',
    interaction: 'interactive',
    priority: 'normal',
    sources: { agent: 'default', model: 'default', reasoning: 'default', interaction: 'default', priority: 'default' },
  };
  for (const l of levels) { if (l.config.runner) { r.runner = l.config.runner; break; } }
  for (const l of levels) {
    if (l.config.agent) {
      r.agent = l.config.agent;
      r.sources.agent = l.source;
      break;
    }
  }
  for (const l of levels) {
    if (l.config.interaction) {
      r.interaction = l.config.interaction;
      r.sources.interaction = l.source;
      break;
    }
  }
  for (const l of levels) {
    if (l.config.priority) {
      r.priority = l.config.priority;
      r.sources.priority = l.source;
      break;
    }
  }
  for (const l of levels) {
    if ((l.config.agent ?? '') !== r.agent) continue;
    if (l.config.model) {
      r.sources.model = l.source;
      if (l.config.model !== AGENT_DEFAULT) r.model = l.config.model;
      break;
    }
  }
  for (const l of levels) {
    if ((l.config.agent ?? '') !== r.agent) continue;
    if (l.config.reasoning) {
      r.sources.reasoning = l.source;
      if (l.config.reasoning !== AGENT_DEFAULT) r.reasoning = l.config.reasoning;
      break;
    }
  }
  return r;
}

/** The levels above a task, for resolving what a task (or a new one) would get. */
export function resolveFor(task: ExecutionConfig | undefined, project: ExecutionConfig | undefined, global: ExecutionConfig | undefined, run?: ExecutionConfig): Resolved {
  return resolveExecution(
    { source: 'run', config: run ?? {} },
    { source: 'task', config: task ?? {} },
    { source: 'project', config: project ?? {} },
    { source: 'global', config: global ?? {} },
  );
}

// ---- wording ----

export const PRIORITY_OPTIONS: readonly { value: Priority; label: string }[] = [
  { value: 'low', label: 'Low' },
  { value: 'normal', label: 'Normal' },
  { value: 'high', label: 'High' },
];

export function priorityLabel(p: Priority | undefined): string {
  return PRIORITY_OPTIONS.find((o) => o.value === p)?.label ?? 'Normal';
}

/** Where a setting comes from, in the user's words. */
export function sourceLabel(s: Source): string {
  return { run: 'this run', task: 'this task', project: 'the project', global: 'your defaults', default: 'the built-in default' }[s];
}

/** The agent's display name for an ID, or the ID if it is not known. */
export function agentLabel(agents: readonly Agent[], id: string): string {
  return agents.find((a) => a.id === id)?.name ?? id;
}

/** The label of a model or reasoning option; an unlisted value is shown as it is. */
export function optionLabel(options: readonly AgentOption[] | undefined, id: string): string {
  if (!id || id === AGENT_DEFAULT) return 'Agent default';
  return options?.find((o) => o.id === id)?.name ?? id;
}

export interface Summary {
  agent: string;
  model: string;
  reasoning: string;
  interaction: InteractionPolicy;
  priority: Priority;
}

/**
 * What a resolved configuration says on a card or a line: "Claude Code · Sonnet · High". Parts that
 * are the agent's own default are left out, so a task nobody configured says nothing at all.
 */
export function summaryLine(r: Resolved, agents: readonly Agent[], options: ReadonlyMap<string, AgentOptions>): string {
  const parts: string[] = [];
  if (r.agent) parts.push(agentLabel(agents, r.agent));
  if (r.model) parts.push(optionLabel(options.get(r.agent)?.models, r.model));
  if (r.reasoning) parts.push(optionLabel(options.get(r.agent)?.reasoning, r.reasoning));
  return parts.join(' · ');
}

/** Whether a task's own overrides are worth a mention on its card. */
export function hasOverrides(c: ExecutionConfig | undefined): boolean {
  return !!c && !!(c.runner || c.agent || c.model || c.reasoning || c.interaction || c.priority);
}

/** What the model picker offers for an agent: its list, and whether a name may be typed. */
export function modelChoices(options: AgentOptions | undefined): { list: AgentOption[]; custom: boolean } {
  return { list: options?.models ?? [{ id: AGENT_DEFAULT, name: 'Agent default' }], custom: options?.customModels ?? true };
}

/**
 * The reasoning levels the picker offers once a model is chosen: those the model supports when the
 * agent says (always with the agent default first), else all the agent's.
 */
export function reasoningChoices(options: AgentOptions | undefined, model: string): AgentOption[] {
  const all = options?.reasoning ?? [{ id: AGENT_DEFAULT, name: 'Agent default' }];
  const m = options?.models.find((x) => x.id === model);
  if (!m?.reasoning?.length) return all;
  return all.filter((o) => o.id === AGENT_DEFAULT || m.reasoning!.includes(o.id));
}

/** A config with empty fields removed, as it is stored: nothing set means inherit. */
export function compact(c: ExecutionConfig): ExecutionConfig {
  const out: ExecutionConfig = {};
 if (c.runner) out.runner=c.runner;
  if (c.agent) out.agent = c.agent;
  // A model and a reasoning level belong to an agent; without one they cannot be kept.
  if (c.agent && c.model) out.model = c.model;
  if (c.agent && c.reasoning) out.reasoning = c.reasoning;
  if (c.interaction) out.interaction = c.interaction;
  if (c.priority) out.priority = c.priority;
  return out;
}

/** Whether a model name is shaped like one, as the controller will insist: nothing that could pass for a flag. */
export function validModelName(s: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._:/[\]+@-]{0,99}$/.test(s);
}

/** Availability is evaluated in the chosen execution environment, including inherited runners. */
export function eligibleRunners(runners: readonly Runner[], projectId: string, runner = '', includeBusy = false): Runner[] {
  return runners.filter(r => r.online && !r.disabled && (includeBusy || r.currentRuns < r.capacity) &&
    (runner && runner !== 'automatic' ? r.id === runner : r.automatic) &&
    (r.kind === 'local' || (!projectId || r.projects.includes(projectId)) &&
      (!projectId && (r.capabilities?.repositories.length ?? 0) > 0 || r.capabilities?.repositories.includes(projectId) || r.allowClone && r.capabilities?.cloneEnabled)));
}
export function executionAgents(agents: readonly Agent[], runners: readonly Runner[], projectId: string, runner = '', includeBusy = false): Agent[] {
  if (runners.length === 0) return agents.filter(a => a.available);
  const found = new Map<string, Agent>();
  for (const r of eligibleRunners(runners, projectId, runner, includeBusy)) for (const a of r.capabilities?.agents ?? []) if (a.available) found.set(a.id, a);
  return [...found.values()];
}
