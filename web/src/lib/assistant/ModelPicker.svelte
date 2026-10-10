<script lang="ts">
  import Icon from '../Icon.svelte';
  import { modelName, reasoningLevels, withModel } from './choice';
  import { assistant } from './store.svelte';

  // Which assistant answers: the provider, the model and how hard it thinks. It can be changed at any time, so that someone whose
  // plan cannot use a model can pick one that works. Another provider starts a fresh conversation with it, and this says so first.

  let panel: HTMLDivElement | undefined = $state();
  let button: HTMLButtonElement | undefined = $state();

  const choice = $derived(assistant.choice);
  const current = $derived(assistant.provider);
  const busy = $derived(assistant.running);
  const levels = $derived(choice ? reasoningLevels(current, choice.model) : []);
  const models = $derived(current?.models.models ?? []);
  const known = $derived(!choice?.model || models.some((m) => m.id === choice.model));
  let custom = $state('');

  function onOutside(e: PointerEvent) {
    if (!assistant.pickerOpen) return;
    const t = e.target as Node;
    if (panel?.contains(t) || button?.contains(t)) return;
    assistant.pickerOpen = false;
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && assistant.pickerOpen) {
      e.preventDefault();
      e.stopPropagation();
      assistant.pickerOpen = false;
      button?.focus();
    }
  }
  $effect(() => {
    if (assistant.pickerOpen) panel?.querySelector<HTMLElement>('select, button:not(:disabled)')?.focus();
  });
</script>

<svelte:window onpointerdown={onOutside} onkeydowncapture={onKey} />

{#if choice}
  <div class="picker">
    <button bind:this={button} class="trigger" type="button" aria-haspopup="dialog" aria-expanded={assistant.pickerOpen} onclick={() => (assistant.pickerOpen = !assistant.pickerOpen)} data-testid="assistant-picker">
      <span class="name">{current?.name ?? choice.provider}</span>
      <span class="sep" aria-hidden="true">·</span>
      <span class="model">{modelName(current, choice.model)}</span>
      {#if choice.reasoning}<span class="sep" aria-hidden="true">·</span><span class="model">{choice.reasoning}</span>{/if}
      <Icon name="down" size={12} />
      <span class="visually-hidden">Change provider, model or thinking level</span>
    </button>

    {#if assistant.pickerOpen}
      <div bind:this={panel} class="card panel" role="dialog" aria-label="Provider, model and thinking" data-testid="assistant-picker-panel">
        <div class="field">
          <span class="lab" id="asst-prov">Provider</span>
          <div class="seg" role="group" aria-labelledby="asst-prov">
            {#each assistant.providers as p (p.id)}
              <button type="button" aria-pressed={p.id === choice.provider} disabled={busy || (!p.available && p.id !== choice.provider)} title={p.available ? '' : (p.detail ?? 'Not ready')} onclick={() => assistant.configure({ provider: p.id })}>
                {p.name}
              </button>
            {/each}
          </div>
          {#each assistant.providers.filter((p) => !p.available) as p (p.id)}
            <p class="hint">{p.name}: {p.detail ?? 'not ready'}. {p.guidance ?? ''}</p>
          {/each}
          {#if current && assistant.session && assistant.session.provider !== choice.provider}
            <p class="hint">Another provider starts a fresh conversation with it.</p>
          {:else if assistant.hasConversation}
            <p class="hint">Switching provider starts a fresh conversation with it. The other one’s memory of this stays with it.</p>
          {/if}
          {#if current?.auth}<p class="hint">Signed in with {current.auth}. Werkbord never asks for a key.</p>{/if}
          {#if current?.usesApiKey}<p class="hint warn">An API key in your environment is being used, so this is billed to that key instead of your plan.</p>{/if}
        </div>

        <label class="field">
          <span class="lab">Model</span>
          <select class="select" aria-label="Model" disabled={busy} value={known ? choice.model : '__other'} onchange={(e) => {
            const v = e.currentTarget.value;
            if (v !== '__other') void assistant.configure(withModel(current, choice, v));
          }}>
            <option value="">Default{models.find((m) => m.default) ? ` (${models.find((m) => m.default)?.name})` : ''}</option>
            {#each models as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
            {#if !known}<option value="__other">{choice.model}</option>{/if}
          </select>
          {#if current?.models.note}<span class="hint">{current.models.note}</span>{/if}
        </label>

        {#if current?.models.custom}
          <form class="field" onsubmit={(e) => { e.preventDefault(); const v = custom.trim(); if (v) { void assistant.configure(withModel(current, choice, v)); custom = ''; } }}>
            <label class="lab" for="asst-custom">Another model name</label>
            <div class="row">
              <input id="asst-custom" class="input" bind:value={custom} disabled={busy} placeholder="a model your plan has" autocomplete="off" autocapitalize="off" spellcheck="false" />
              <button class="btn" type="submit" disabled={busy || !custom.trim()}>Use</button>
            </div>
          </form>
        {/if}

        {#if levels.length > 0}
          <label class="field">
            <span class="lab">Thinking</span>
            <select class="select" aria-label="Thinking level" disabled={busy} value={choice.reasoning} onchange={(e) => assistant.configure({ reasoning: e.currentTarget.value })}>
              <option value="">Default</option>
              {#each levels as l (l)}<option value={l}>{l}</option>{/each}
            </select>
          </label>
        {/if}

        {#if busy}<p class="hint">You can change this when the reply has finished.</p>{/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .picker {
    position: relative;
    min-width: 0;
  }
  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    min-height: 36px;
    padding: 0 10px;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text-2);
    font-size: 13px;
    cursor: pointer;
  }
  .trigger:hover,
  .trigger[aria-expanded='true'] {
    background: var(--surface-2);
    color: var(--text);
  }
  .name {
    color: var(--text);
    font-weight: 500;
    white-space: nowrap;
  }
  .model {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sep {
    opacity: 0.6;
  }
  .panel {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 5;
    width: min(22rem, calc(100vw - 32px));
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 14px;
    box-shadow: var(--win-sh);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .hint {
    font-size: 12px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }
  .hint.warn {
    color: var(--warn-text);
  }
  .row {
    display: flex;
    gap: 8px;
  }
  .seg {
    flex-wrap: wrap;
  }
  .seg button:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  @media (max-width: 899px) {
    .panel {
      position: fixed;
      left: 12px;
      right: 12px;
      top: calc(56px + env(safe-area-inset-top));
      width: auto;
      max-height: calc(100dvh - 80px);
      overflow-y: auto;
    }
  }
</style>
