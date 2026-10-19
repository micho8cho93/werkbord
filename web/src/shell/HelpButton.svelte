<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import { model } from './model.svelte';

  // Help is where people look for it: the bottom right corner, one control, the same wherever they are. It opens Individual's
  // guides and keyboard shortcuts, which describe how Werkbord works in every part of it.
  const unavailable = $derived(model.view?.items.find((i) => i.id === 'personal')?.state === 'unavailable');
</script>

{#if model.page !== 'help' && !unavailable}
  <button class="help" type="button" onclick={() => model.openHelp()} aria-label="Help and keyboard shortcuts" title="Help and keyboard shortcuts" data-testid="help-button">
    <Icon name="help" size={15} /><span>Help</span>
  </button>
{/if}

<style>
  .help {
    position: absolute;
    z-index: 6;
    right: 16px;
    bottom: 16px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 32px;
    padding: 0 12px 0 10px;
    border: 1px solid var(--border);
    border-radius: 16px;
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--win-sh);
    font: inherit;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
  }
  .help:hover {
    background: var(--surface-2);
  }
  .help:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .help :global(svg) {
    color: var(--text-2);
  }
</style>
