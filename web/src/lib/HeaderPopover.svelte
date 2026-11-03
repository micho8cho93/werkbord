<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';

  // One anchored popover for the header: a button that says what it holds, and a panel under it. Not modal: the page stays reachable,
  // Escape or a click elsewhere closes it and focus goes back to the button.
  let { label, expandedLabel, children, trigger, align = 'end', testid }: {
    label: string; expandedLabel?: string; children: Snippet; trigger: Snippet; align?: 'start' | 'end'; testid?: string;
  } = $props();
  let open = $state(false);
  let root = $state<HTMLElement>();
  let button = $state<HTMLButtonElement>();
  const close = (refocus = true) => { open = false; if (refocus) button?.focus(); };
  function away(e: PointerEvent) { if (open && root && !root.contains(e.target as Node)) close(false); }
</script>

<svelte:window onpointerdown={away} onkeydown={(e) => { if (e.key === 'Escape' && open) { e.stopPropagation(); close(); } }} />
<div class="pop" bind:this={root} data-testid={testid}>
  <button bind:this={button} type="button" class="trigger" aria-expanded={open} aria-haspopup="true" aria-label={open ? (expandedLabel ?? label) : label} onclick={() => (open = !open)}>
    {@render trigger()}<Icon name="down" size={12} />
  </button>
  {#if open}
    <div class="panel" class:start={align === 'start'} role="group" aria-label={label}>
      {@render children()}
    </div>
  {/if}
</div>

<style>
  .pop { position: relative; display: inline-flex; }
  .trigger { display: inline-flex; align-items: center; gap: 6px; min-height: 28px; padding: 2px 8px; border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: inherit; font: inherit; cursor: pointer; white-space: nowrap; }
  .trigger:hover, .trigger[aria-expanded='true'] { background: var(--hover); border-color: var(--border); }
  .trigger:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .panel { position: absolute; z-index: 20; top: calc(100% + 6px); right: 0; min-width: 260px; max-width: min(360px, calc(100vw - 24px)); max-height: min(60vh, 420px); overflow-y: auto; padding: 8px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); color: var(--text); box-shadow: var(--win-sh); font-size: 13px; }
  .panel.start { right: auto; left: 0; }
</style>
