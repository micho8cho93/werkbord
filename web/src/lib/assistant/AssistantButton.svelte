<script lang="ts">
  import { app } from '../state.svelte';
  import Icon from '../Icon.svelte';
  import { assistant } from './sessions.svelte';
  import { originOf } from './transition';

  // The assistant opens and closes from this button, which can sit in a rail, a page toolbar, or a phone heading. Its badge says
  // whether it is working or has a change waiting for the person.
  let { variant = 'rail' }: { variant?: 'rail' | 'toolbar' | 'icon' } = $props();
  let el: HTMLButtonElement | undefined = $state();

  const waiting = $derived(assistant.waiting);
  const label = $derived(waiting > 0 ? `Salie, ${waiting} ${waiting === 1 ? 'request is' : 'requests are'} waiting for you` : assistant.running ? 'Salie, working' : 'Salie');

  function press() {
    if (el) assistant.toggle(originOf(el.getBoundingClientRect()), el);
  }
</script>

{#if !app.remote}
<button bind:this={el} class="ab {variant}" type="button" aria-haspopup="dialog" aria-expanded={assistant.isOpen} aria-label={label} title="Salie" onclick={press} data-testid="assistant-button">
  <span class="ico"><Icon name="assistant" /></span>
  {#if variant !== 'icon'}<span class="text">Salie</span>{/if}
  {#if waiting > 0}
    <span class="num pend" aria-hidden="true">{waiting}</span>
  {:else if assistant.running}
    <span class="live" aria-hidden="true"></span>
  {/if}
</button>
{/if}

<style>
  .ab {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text);
    font-weight: 500;
    cursor: pointer;
    transition:
      background 0.12s,
      border-color 0.12s;
  }
  .ab:hover {
    background: var(--surface-2);
  }
  .ico {
    display: inline-flex;
    color: var(--accent);
  }
  .rail {
    width: 100%;
    min-height: 36px;
    padding: 0 10px;
    border-radius: var(--radius-sm);
    font-size: 13px;
    box-shadow: var(--card-sh);
  }
  .toolbar {
    min-height: 36px;
    padding: 0 12px;
    border-radius: var(--radius-sm);
    font-size: 13px;
    box-shadow: var(--btn-sh);
  }
  .rail .text {
    flex: 1;
    text-align: left;
  }
  .icon {
    justify-content: center;
    width: 44px;
    height: 44px;
    padding: 0;
    border-radius: var(--radius-sm);
  }
  .live {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 1.4s ease-in-out infinite;
  }
  .icon .live,
  .icon .num {
    position: absolute;
    top: 4px;
    right: 4px;
  }
  .icon .num {
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    font-size: 12px;
  }
</style>
