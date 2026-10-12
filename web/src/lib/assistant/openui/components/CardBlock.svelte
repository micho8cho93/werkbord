<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import type { Snippet } from 'svelte';

  let {
    props,
    renderNode,
  }: ComponentRenderProps<{ title: string; children?: unknown[] }> & { renderNode: Snippet<[unknown]> } = $props();
</script>

<section class="card">
  <h3>{props.title}</h3>
  {#each props.children ?? [] as child, i (i)}
    {@render renderNode(child)}
  {/each}
</section>

<style>
  .card {
    display: grid;
    min-width: 0;
    gap: 8px;
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
  }

  h3 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

</style>
