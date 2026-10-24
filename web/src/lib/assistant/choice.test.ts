import { describe, expect, it } from 'vitest';
import { modelName, pickChoice, reasoningLevels, summary, withModel } from './choice';
import type { ProviderInfo } from './types';

const prov = (id: string, name: string, over: Partial<ProviderInfo> = {}, models: ProviderInfo['models']['models'] = [], custom = true): ProviderInfo => ({
  id, name, installed: true, signedIn: true, available: true,
  capabilities: { streaming: 'tokens', resume: true, cancel: true, modelChoice: true, reasoningChoice: true, voice: 'none' },
  models: { providerId: id, models, custom, reasoning: [] }, ...over,
});
const claude = prov('claude-code', 'Claude Code', {}, [{ id: 'sonnet', name: 'Sonnet' }, { id: 'opus', name: 'Opus' }]);
claude.models.reasoning = ['low', 'high'];
const codex = prov('codex', 'Codex', {}, [{ id: 'gpt-a', name: 'GPT A', default: true, reasoning: ['low', 'medium'] }, { id: 'gpt-b', name: 'GPT B', reasoning: ['high'] }], false);

describe('choosing what answers', () => {
  it('starts with the first provider that is ready', () => {
    expect(pickChoice([prov('x', 'X', { available: false }), claude, codex], null)).toEqual({ provider: 'claude-code', model: '', reasoning: '', mode: 'lowest_cost' });
  });
  it('keeps what the person chose last time when it still works', () => {
    expect(pickChoice([claude, codex], { provider: 'codex', model: 'gpt-b', reasoning: 'high' })).toEqual({ provider: 'codex', model: 'gpt-b', reasoning: 'high' });
  });
  it('drops a model or level that is no longer offered, and a provider that is no longer ready', () => {
    expect(pickChoice([claude, codex], { provider: 'codex', model: 'gone', reasoning: 'low' })).toEqual({ provider: 'codex', model: '', reasoning: 'low' });
    expect(pickChoice([claude, codex], { provider: 'codex', model: 'gpt-b', reasoning: 'low' })).toEqual({ provider: 'codex', model: 'gpt-b', reasoning: '' });
    expect(pickChoice([claude, { ...codex, available: false }], { provider: 'codex', model: 'gpt-a' })).toEqual({ provider: 'claude-code', model: '', reasoning: '' });
  });
  it('lets a typed model name through where the provider allows one', () => {
    expect(pickChoice([claude], { provider: 'claude-code', model: 'claude-custom-1' })?.model).toBe('claude-custom-1');
  });
  it('is nothing when nothing is ready', () => {
    expect(pickChoice([{ ...claude, available: false }], null)).toBeNull();
    expect(pickChoice([], null)).toBeNull();
  });
});

describe('levels and names', () => {
  it('takes the levels from the model, else the default model, else the provider', () => {
    expect(reasoningLevels(codex, 'gpt-b')).toEqual(['high']);
    expect(reasoningLevels(codex, '')).toEqual(['low', 'medium']);
    expect(reasoningLevels(claude, 'opus')).toEqual(['low', 'high']);
    expect(reasoningLevels(undefined, '')).toEqual([]);
  });
  it('drops a level the new model does not take', () => {
    expect(withModel(codex, { provider: 'codex', model: 'gpt-a', reasoning: 'low' }, 'gpt-b')).toEqual({ provider: 'codex', model: 'gpt-b', reasoning: '' });
    expect(withModel(claude, { provider: 'claude-code', model: 'sonnet', reasoning: 'high' }, 'opus').reasoning).toBe('high');
  });
  it('names the model for a person', () => {
    expect(modelName(claude, 'sonnet')).toBe('Sonnet');
    expect(modelName(codex, '')).toBe('GPT A');
    expect(modelName(claude, '')).toBe('Default');
    expect(modelName(claude, 'typed-in')).toBe('typed-in');
    expect(summary(codex, { provider: 'codex', model: 'gpt-b', reasoning: 'high' })).toBe('Codex · GPT B · high');
  });
});
