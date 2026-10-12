<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import type { Snippet } from 'svelte';

  let {
    props,
    renderNode,
  }: ComponentRenderProps<{ title?: string; children?: unknown[] }> & { renderNode: Snippet<[unknown]> } = $props();
</script>

<details class="disclosure">
  <summary>{props.title ?? 'More details'}</summary>
  <div class="content">
    {#each props.children ?? [] as child, i (i)}
      {@render renderNode(child)}
    {/each}
  </div>
</details>

<style>
  .disclosure { min-width: 0; padding: 9px 11px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  summary { display: flex; min-height: 44px; align-items: center; cursor: pointer; font-size: 13px; font-weight: 500; overflow-wrap: anywhere; }
  summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; border-radius: 2px; }
  .content { display: grid; gap: 8px; padding-top: 10px; }
</style>
