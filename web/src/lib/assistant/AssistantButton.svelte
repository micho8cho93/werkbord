<script lang="ts">
  import Icon from '../Icon.svelte';
  import { assistant } from './store.svelte';
  import { originOf } from './transition';

  // The way in. On a wide screen it sits in the rail ("rail"); on a phone it is an icon in the heading ("icon"). Either way the
  // assistant opens out of it and closes back into it, and it says, without opening anything, if the assistant is working or
  // has a change waiting for the person.
  let { variant = 'rail' }: { variant?: 'rail' | 'icon' } = $props();
  let el: HTMLButtonElement | undefined = $state();

  const waiting = $derived(assistant.waiting);
  const label = $derived(waiting > 0 ? `Assistant, ${waiting} ${waiting === 1 ? 'change is' : 'changes are'} waiting for you` : assistant.running ? 'Assistant, working' : 'Assistant');

  function press() {
    if (el) assistant.toggle(originOf(el.getBoundingClientRect()), el);
  }
</script>

<button bind:this={el} class="ab {variant}" type="button" aria-haspopup="dialog" aria-expanded={assistant.isOpen} aria-label={label} title="Assistant" onclick={press} data-testid="assistant-button">
  <span class="ico"><Icon name="assistant" /></span>
  {#if variant === 'rail'}<span class="text">Assistant</span>{/if}
  {#if waiting > 0}
    <span class="num pend" aria-hidden="true">{waiting}</span>
  {:else if assistant.running}
    <span class="live" aria-hidden="true"></span>
  {/if}
</button>

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
    font-size: 10.5px;
  }
</style>
