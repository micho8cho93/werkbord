<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';

  let { props }: ComponentRenderProps<{ title?: string; items?: string[] }> = $props();
</script>

<section class="list">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <ul>{#each props.items ?? [] as item, i (i)}<li>{item}</li>{/each}</ul>
</section>

<style>
  .list { min-width: 0; }
  h3 { margin: 0 0 6px; font-size: 13px; font-weight: 600; }
  ul { margin: 0; padding-left: 1.25em; display: grid; gap: 5px; }
  li { padding-left: 2px; font-size: 13px; line-height: 1.45; overflow-wrap: anywhere; }
</style>
