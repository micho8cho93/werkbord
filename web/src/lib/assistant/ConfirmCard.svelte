<script lang="ts">
  import Icon from '../Icon.svelte';
  import { assistant } from './store.svelte';
  import type { ConfirmItem } from './transcript';
  import { clock, operationTitle } from './words';

  // A change the assistant prepared. Nothing has happened: the words below were written by Werkbord from the exact arguments
  // that will be used, not by the model, and only the button here carries the change out.
  let { item }: { item: ConfirmItem } = $props();
  const until = $derived(Date.parse(item.expiresAt));
</script>

<section class="card confirm" data-state={item.state} aria-label="{operationTitle(item.operation)}: {item.state === 'waiting' ? 'waiting for you' : item.state}">
  <div class="lab">{operationTitle(item.operation)}</div>
  <p class="sum">{item.summary}</p>

  {#if item.state === 'waiting' || item.state === 'working'}
    <div class="acts">
      <button class="btn primary" type="button" disabled={item.state === 'working'} onclick={() => assistant.resolve(item, 'approve')}>
        {item.state === 'working' ? 'Working…' : 'Confirm'}
      </button>
      <button class="btn" type="button" disabled={item.state === 'working'} onclick={() => assistant.resolve(item, 'decline')}>Not now</button>
    </div>
    <p class="fine">Nothing changes until you confirm.{Number.isFinite(until) ? ` Open until ${clock(until)}.` : ''}</p>
  {:else if item.state === 'confirmed'}
    <p class="result ok"><Icon name="check" size={14} /> Done.{item.outcome ? ` ${item.outcome}` : ''}</p>
  {:else if item.state === 'declined'}
    <p class="result">Not done. You said no.</p>
  {:else if item.state === 'expired'}
    <p class="result">Not confirmed in time, so nothing changed. Ask again if you still want it.</p>
  {:else if item.state === 'failed'}
    <p class="result bad" role="alert">This could not be done.{item.outcome ? ` ${item.outcome}` : ''} Nothing was changed by the attempt unless it says otherwise.</p>
  {:else}
    <p class="result">No longer waiting. It was answered, or it timed out, while you were away.</p>
  {/if}
</section>

<style>
  .confirm {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px 16px;
    border-left: 3px solid var(--border);
  }
  .confirm[data-state='waiting'],
  .confirm[data-state='working'] {
    border-left-color: var(--amber);
  }
  .confirm[data-state='confirmed'] {
    border-left-color: var(--ok);
  }
  .confirm[data-state='failed'] {
    border-left-color: var(--danger);
  }
  .sum {
    font-size: 14px;
    overflow-wrap: anywhere;
  }
  .acts {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    padding-top: 2px;
  }
  .fine,
  .result {
    font-size: 12.5px;
    color: var(--text-2);
  }
  .result {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .result.ok {
    color: var(--ok-text);
  }
  .result.bad {
    color: var(--danger-text);
    align-items: flex-start;
  }
</style>
