// Which provider, model and reasoning level a conversation uses, and what can be chosen. The assistant borrows the person's own
// signed-in Claude Code, Codex, Gemini CLI or OpenCode; nothing here asks for a key or an account.

import type { ProviderInfo, ProviderModel } from './types';

export interface Choice {
  provider: string;
  /** Empty is the provider's own default. */
  model: string;
  reasoning: string;
}

export const modelOf = (p: ProviderInfo | undefined, id: string): ProviderModel | undefined => p?.models.models.find((m) => m.id === id);

/** The levels the chosen model takes: its own, or the provider's, or none where it does not say. */
export function reasoningLevels(p: ProviderInfo | undefined, modelId: string): string[] {
  if (!p) return [];
  const m = modelId ? modelOf(p, modelId) : p.models.models.find((x) => x.default);
  if (m?.reasoning?.length) return m.reasoning;
  return p.models.reasoning ?? [];
}

/**
 * The choice to start with: what the person chose last time if it still works, otherwise the first provider that is ready.
 * Null when none is ready, which is the "set up a supported CLI" state.
 */
export function pickChoice(providers: ProviderInfo[], saved: Partial<Choice> | null): Choice | null {
  const ready = providers.filter((p) => p.available);
  if (ready.length === 0) return null;
  const p = ready.find((x) => x.id === saved?.provider) ?? ready[0];
  const same = saved?.provider === p.id;
  let model = same ? (saved?.model ?? '') : '';
  if (model && !modelOf(p, model) && !p.models.custom) model = '';
  let reasoning = same ? (saved?.reasoning ?? '') : '';
  if (reasoning && !reasoningLevels(p, model).includes(reasoning)) reasoning = '';
  return { provider: p.id, model, reasoning };
}

/** A model's name for the person: its display name, "Default" for none, the id for one typed in. */
export function modelName(p: ProviderInfo | undefined, id: string): string {
  if (!id) {
    const d = p?.models.models.find((m) => m.default);
    return d ? d.name : 'Default';
  }
  return modelOf(p, id)?.name ?? id;
}

/** Changing the model keeps the level if the new model takes it, and otherwise goes back to the default. */
export function withModel(p: ProviderInfo | undefined, c: Choice, model: string): Choice {
  const next = { ...c, model };
  if (next.reasoning && !reasoningLevels(p, model).includes(next.reasoning)) next.reasoning = '';
  return next;
}

/** A one-line account of the choice, for the header. */
export function summary(p: ProviderInfo | undefined, c: Choice): string {
  if (!p) return '';
  const bits = [p.name, modelName(p, c.model)];
  if (c.reasoning) bits.push(c.reasoning);
  return bits.join(' · ');
}
