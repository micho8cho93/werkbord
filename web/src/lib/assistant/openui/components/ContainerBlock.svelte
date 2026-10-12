<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import type { Snippet } from 'svelte';

  let {
    props,
    renderNode,
  }: ComponentRenderProps<{ children?: unknown[]; direction?: 'vertical' | 'horizontal' | 'grid'; gap?: 'small' | 'medium' | 'large'; columns?: 2 | 3 }> & {
    renderNode: Snippet<[unknown]>;
  } = $props();
</script>

<div class="container" data-direction={props.direction ?? 'vertical'} data-gap={props.gap ?? 'medium'} data-columns={props.columns ?? 2}>
  {#each props.children ?? [] as child, i (i)}
    {@render renderNode(child)}
  {/each}
</div>

<style>
  .container {
    display: grid;
    min-width: 0;
    gap: 12px;
  }

  .container[data-gap='small'] { gap: 7px; }
  .container[data-gap='large'] { gap: 18px; }
  .container[data-direction='horizontal'] {
    grid-auto-flow: column;
    grid-auto-columns: minmax(0, 1fr);
    align-items: start;
  }
  .container[data-direction='grid'] {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .container[data-direction='grid'][data-columns='3'] { grid-template-columns: repeat(3, minmax(0, 1fr)); }

  @media (max-width: 520px) {
    .container[data-direction='horizontal'] { grid-auto-flow: row; grid-auto-columns: auto; }
    .container[data-direction='grid'] { grid-template-columns: 1fr; }
  }
</style>
