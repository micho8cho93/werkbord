<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import type { Snippet } from 'svelte';

  let {
    props,
    renderNode,
  }: ComponentRenderProps<{ children?: unknown[]; columns?: 2 | 3; gap?: 'small' | 'medium' | 'large' }> & {
    renderNode: Snippet<[unknown]>;
  } = $props();
</script>

<div class="grid" data-gap={props.gap ?? 'medium'} data-columns={props.columns ?? 2}>
  {#each props.children ?? [] as child, i (i)}
    {@render renderNode(child)}
  {/each}
</div>

<style>
  .grid {
    display: grid;
    min-width: 0;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }

  .grid[data-gap='small'] { gap: 7px; }
  .grid[data-gap='large'] { gap: 18px; }
  .grid[data-columns='3'] { grid-template-columns: repeat(3, minmax(0, 1fr)); }

  @media (max-width: 520px) {
    .grid { grid-template-columns: 1fr; }
  }
</style>
