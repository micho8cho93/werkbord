<script lang="ts">
  import Icon from '../Icon.svelte';
  import Mark from '../Mark.svelte';
  import { globalHref } from '../router.svelte';
  import { embedded } from '../embed';
  import { onSwitcher, openSwitcher } from '../../../../internal/nativebridge/bridge.js';

  // The product name at the top of the rail. On its own it is the way home (the Control Center). Inside the Werkbord desktop
  // app it is also the switcher between Individual and Team, as in other apps that hold two modes in one window: clicking it
  // opens the app's menu under it. Nothing but where the name is goes to the app. On a narrow window, where the rail is
  // folded away, the compact variant (the mark alone) does the same from the page's own header.
  let { variant = 'rail' }: { variant?: 'rail' | 'compact' } = $props();

  const framed = embedded();
  let button: HTMLButtonElement | undefined = $state();
  let open = $state(false);

  $effect(() => {
    if (!framed) return;
    return onSwitcher((s) => {
      open = s.open;
      if (!s.open && s.focus) button?.focus();
    });
  });
</script>

{#if framed}
  <button bind:this={button} class="switch {variant}" type="button" aria-haspopup="menu" aria-expanded={open} aria-label="Switch between Individual and Team. Showing Individual" onclick={() => openSwitcher(button)} data-testid="product-switch">
    {#if variant === 'rail'}
      <span class="brand-row"><Mark height={20} /><span class="wm">werkbord</span></span>
      <span class="mode-row"><span class="mode">Individual</span><Icon name="down" size={14} /></span>
    {:else}
      <Mark height={18} /><Icon name="down" size={12} />
    {/if}
  </button>
{:else if variant === 'rail'}
  <a class="brand" href={globalHref('control')} aria-label="Werkbord, Control Center">
    <Mark height={20} />
    <span class="wm">werkbord</span>
  </a>
{:else}
  <Mark height={18} />
{/if}

<style>
  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px 8px 16px;
    color: var(--text);
    text-decoration: none;
  }
  .switch {
    display: flex;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .switch.rail {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    width: 100%;
    margin-bottom: 10px;
    padding: 6px 8px;
  }
  .switch.compact {
    align-items: center;
    gap: 2px;
    padding: 4px;
    color: var(--text-2);
  }
  .switch:hover,
  .switch[aria-expanded='true'] {
    background: var(--surface-2);
  }
  .switch:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .brand-row,
  .mode-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .mode-row {
    gap: 6px;
    color: var(--text-2);
    font-size: 13px;
    font-weight: 500;
  }
  .wm {
    font-family: var(--mono);
    font-weight: 600;
    font-size: 17px;
    letter-spacing: -0.06em;
    line-height: 1;
  }
</style>
