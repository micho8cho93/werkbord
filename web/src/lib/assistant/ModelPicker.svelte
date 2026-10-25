<script lang="ts">
  import Icon from '../Icon.svelte';
 import { SELECTION_MODES, modeLabel, type SelectionMode } from '../selection';
  import { modelName, reasoningLevels, withModel } from './choice';
  import type { AssistantStore } from './store.svelte';
  let { assistant }: { assistant: AssistantStore } = $props();

  // Which assistant answers: the provider, the model and how hard it thinks. It can be changed at any time, so that someone whose
  // plan cannot use a model can pick one that works. Another provider starts a fresh conversation with it, and this says so first.

  let panel: HTMLDivElement | undefined = $state();
  let button: HTMLButtonElement | undefined = $state();

  const choice = $derived(assistant.choice);
  const current = $derived(assistant.provider);
  const busy = $derived(assistant.running);
 const mode=$derived(choice?.mode ?? 'manual');
 async function choose(patch:Parameters<typeof assistant.configure>[0],close=true){await assistant.configure(patch);if(close)assistant.pickerOpen=false;}
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
      <span class="name">{mode==='manual'?(current?.name ?? choice.provider):modeLabel(mode)}</span>
      <span class="sep" aria-hidden="true">·</span>
      <span class="model">{mode==='manual'||assistant.session?.selection?modelName(current, choice.model):'Choose at conversation start'}</span>
      {#if choice.reasoning}<span class="sep" aria-hidden="true">·</span><span class="model">{choice.reasoning}</span>{/if}
      <Icon name="down" size={12} />
      <span class="visually-hidden">Change provider, model or reasoning level</span>
    </button>

    {#if assistant.pickerOpen}
      <div bind:this={panel} class="card panel" role="dialog" aria-label="Provider, model and reasoning level" data-testid="assistant-picker-panel">
        <label class="field"><span class="lab">Model selection</span>
         <select class="select" aria-label="Model selection" value={mode} disabled={busy} onchange={e=>choose({mode:e.currentTarget.value as SelectionMode},e.currentTarget.value!=='manual')}>
          {#each SELECTION_MODES as option (option.value)}<option value={option.value} disabled={option.value!=='manual'&&assistant.hasConversation&&option.value!==mode}>{option.label}</option>{/each}
         </select><span class="hint">{SELECTION_MODES.find(x=>x.value===mode)?.description}</span>
        </label>
        {#if mode !== 'manual'}
         <p class="hint">Uses eligible agents enabled in Settings → Agents. Automatic selection stays fixed for this conversation.</p>
         {#if assistant.session?.selection}<p class="hint">{assistant.session.selection.reason} ({assistant.session.selection.method==='classifier'?'AI classifier':'Local rules fallback'})</p>{/if}
        {:else}
        <div class="field">
          <label class="lab" for="asst-prov">Agent</label>
          <select id="asst-prov" class="select" disabled={busy} value={choice.provider} onchange={e=>choose({provider:e.currentTarget.value},false)}>
           {#each assistant.providers.filter(p=>p.available||p.id===choice.provider) as p(p.id)}<option value={p.id} disabled={!p.available}>{p.name}</option>{/each}
          </select>
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
            if (v !== '__other') void choose(withModel(current, choice, v));
          }}>
            <option value="">Default{models.find((m) => m.default) ? ` (${models.find((m) => m.default)?.name})` : ''}</option>
            {#each models as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
            {#if !known}<option value="__other">{choice.model}</option>{/if}
          </select>
          {#if current?.models.note}<span class="hint">{current.models.note}</span>{/if}
        </label>

        {#if current?.models.custom}
          <form class="field" onsubmit={(e) => { e.preventDefault(); const v = custom.trim(); if (v) { void choose(withModel(current, choice, v)); custom = ''; } }}>
            <label class="lab" for="asst-custom">Another model name</label>
            <div class="row">
              <input id="asst-custom" class="input" bind:value={custom} disabled={busy} placeholder="a model your plan has" autocomplete="off" autocapitalize="off" spellcheck="false" />
              <button class="btn" type="submit" disabled={busy || !custom.trim()}>Use</button>
            </div>
          </form>
        {/if}

        {#if levels.length > 0}
          <label class="field">
            <span class="lab">Reasoning level</span>
            <select class="select" aria-label="Reasoning level" disabled={busy} value={choice.reasoning} onchange={(e) => choose({ reasoning: e.currentTarget.value })}>
              <option value="">Default</option>
              {#each levels as l (l)}<option value={l}>{l}</option>{/each}
            </select>
          </label>
        {/if}

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
