<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import Mark from '../lib/Mark.svelte';
  import { model } from './model.svelte';

  // The product name on the shell's own pages (My Work, Calendar…), where there is no workspace header to click: the same
  // switcher, opened under it.
  const active = $derived(model.view?.items.find((i) => i.id === model.current));
  let button: HTMLButtonElement | undefined = $state();

  function toggle(): void {
    if (model.switcher) return model.closeSwitcher();
    const r = button!.getBoundingClientRect();
    model.openSwitcher({ x: r.left, y: r.top, width: r.width, height: r.height, from: null });
  }
</script>

<button bind:this={button} class="name" type="button" aria-haspopup="menu" aria-expanded={!!model.switcher} onclick={toggle} data-testid="workspace-switcher">
  <Mark height={18} />
  <span class="label">{active?.name ?? 'Individual'}</span>
  <Icon name="down" size={14} />
</button>

<style>
  .name {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    max-width: 100%;
    min-height: 34px;
    padding: 0 10px 0 8px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text);
    font: inherit;
    font-weight: 600;
    font-size: 14px;
    cursor: pointer;
  }
  .name:hover,
  .name[aria-expanded='true'] {
    background: var(--surface-2);
  }
  .name:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
