import { describe, expect, it } from 'vitest';
import {
  AGENT_DEFAULT,
  compact,
  hasOverrides,
  modelChoices,
  reasoningChoices,
  resolveExecution,
  resolveFor,
  summaryLine,
  validModelName,
} from './execution';
import type { Agent, AgentOptions } from './types';

const agents: Agent[] = [
  { id: 'claude-code', name: 'Claude Code', installed: true, available: true },
  { id: 'codex', name: 'Codex', installed: true, available: true },
];

describe('resolveExecution', () => {
  it('has the built-in defaults when nothing is set', () => {
    const r = resolveFor(undefined, undefined, undefined);
    expect(r).toMatchObject({ agent: '', model: '', reasoning: '', interaction: 'interactive', priority: 'normal' });
    expect(Object.values(r.sources)).toEqual(['default', 'default', 'default', 'default', 'default']);
  });

  it('lets a task override a project, which overrides the global defaults', () => {
    const global = { agent: 'claude-code', model: 'sonnet', reasoning: 'medium', interaction: 'autonomous', priority: 'low' } as const;
    const project = { agent: 'claude-code', model: 'opus', priority: 'high' } as const;
    const task = { interaction: 'interactive' } as const;
    const r = resolveFor(task, project, global);
    expect(r).toMatchObject({ agent: 'claude-code', model: 'opus', reasoning: 'medium', interaction: 'interactive', priority: 'high' });
    expect(r.sources).toEqual({ agent: 'project', model: 'project', reasoning: 'global', interaction: 'task', priority: 'project' });
  });

  it('lets a run beat the task', () => {
    const r = resolveFor({ interaction: 'interactive' }, {}, {}, { interaction: 'autonomous_stop_if_blocked' });
    expect(r.interaction).toBe('autonomous_stop_if_blocked');
    expect(r.sources.interaction).toBe('run');
  });

  it('lets a task ask for the agent default over an inherited model', () => {
    const r = resolveFor({ agent: 'codex', model: AGENT_DEFAULT, reasoning: AGENT_DEFAULT }, { agent: 'codex', model: 'gpt-x', reasoning: 'high' }, {});
    expect(r.model).toBe('');
    expect(r.reasoning).toBe('');
    expect(r.sources.model).toBe('task');
  });

  it("never hands one agent's model to another", () => {
    const project = { agent: 'claude-code', model: 'opus', reasoning: 'high' };
    const r = resolveFor({ agent: 'codex' }, project, { agent: 'claude-code', model: 'sonnet' });
    expect(r.agent).toBe('codex');
    expect(r.model).toBe('');
    expect(r.reasoning).toBe('');
    // The task's own model for its own agent is used.
    expect(resolveFor({ agent: 'codex', model: 'gpt-x' }, project, {}).model).toBe('gpt-x');
    // A task that names the same agent as the project still inherits the project's model.
    expect(resolveFor({ agent: 'claude-code' }, project, {}).model).toBe('opus');
  });

  it('takes the project agent with its model when the task names none', () => {
    const r = resolveExecution(
      { source: 'task', config: {} },
      { source: 'project', config: { agent: 'codex', model: 'gpt-x' } },
      { source: 'global', config: { agent: 'claude-code', model: 'sonnet' } },
    );
    expect(r.agent).toBe('codex');
    expect(r.model).toBe('gpt-x');
  });
});

describe('wording and choices', () => {
  const options = new Map<string, AgentOptions>([
    [
      'claude-code',
      {
        agentId: 'claude-code',
        models: [
          { id: 'default', name: 'Agent default' },
          { id: 'sonnet', name: 'Sonnet' },
        ],
        reasoning: [
          { id: 'default', name: 'Agent default' },
          { id: 'high', name: 'High' },
        ],
        modelsSource: 'builtin',
        reasoningSource: 'agent',
        customModels: true,
      },
    ],
  ]);

  it('says nothing for a task nobody configured', () => {
    expect(summaryLine(resolveFor(undefined, undefined, undefined), agents, options)).toBe('');
  });

  it('names the agent, model and reasoning, and shows an unlisted model as it is', () => {
    expect(summaryLine(resolveFor({ agent: 'claude-code', model: 'sonnet', reasoning: 'high' }, {}, {}), agents, options)).toBe('Claude Code · Sonnet · High');
    expect(summaryLine(resolveFor({ agent: 'claude-code', model: 'claude-opus-9' }, {}, {}), agents, options)).toBe('Claude Code · claude-opus-9');
  });

  it('offers only the reasoning levels a model supports, when the agent says', () => {
    const o: AgentOptions = {
      agentId: 'codex',
      models: [
        { id: 'default', name: 'Agent default' },
        { id: 'small', name: 'Small', reasoning: ['low'] },
        { id: 'big', name: 'Big', reasoning: ['low', 'high'] },
        { id: 'unknown', name: 'Unknown' },
      ],
      reasoning: [
        { id: 'default', name: 'Agent default' },
        { id: 'low', name: 'Low' },
        { id: 'high', name: 'High' },
      ],
      modelsSource: 'agent',
      reasoningSource: 'agent',
      customModels: true,
    };
    expect(reasoningChoices(o, 'small').map((x) => x.id)).toEqual(['default', 'low']);
    expect(reasoningChoices(o, 'big').map((x) => x.id)).toEqual(['default', 'low', 'high']);
    expect(reasoningChoices(o, 'unknown').map((x) => x.id)).toEqual(['default', 'low', 'high']);
    expect(reasoningChoices(o, 'typed-by-hand').map((x) => x.id)).toEqual(['default', 'low', 'high']);
    expect(reasoningChoices(undefined, '').map((x) => x.id)).toEqual(['default']);
    expect(modelChoices(undefined)).toEqual({ list: [{ id: 'default', name: 'Agent default' }], custom: true });
  });

  it('stores only what is set, and never a model without an agent', () => {
    expect(compact({})).toEqual({});
    expect(compact({ model: 'x', reasoning: 'high', priority: 'high' })).toEqual({ priority: 'high' });
    expect(compact({ agent: 'codex', model: 'x', reasoning: '', interaction: 'autonomous' })).toEqual({ agent: 'codex', model: 'x', interaction: 'autonomous' });
    expect(hasOverrides({})).toBe(false);
    expect(hasOverrides({ priority: 'low' })).toBe(true);
    expect(hasOverrides(undefined)).toBe(false);
  });

  it('accepts what the controller accepts as a model name', () => {
    for (const ok of ['opus', 'gpt-5.5', 'claude-opus-4-1-20250805', 'sonnet[1m]', 'us.anthropic.claude:0']) expect(validModelName(ok)).toBe(true);
    for (const bad of ['', '--yolo', '-m', 'two words', 'a\nb', ' x', 'x'.repeat(101)]) expect(validModelName(bad)).toBe(false);
  });
});
