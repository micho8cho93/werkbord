<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';

  let { props }: ComponentRenderProps<{ caption?: string; columns?: string[]; rows?: string[][] }> = $props();
</script>

<!-- The scrollable table needs to be keyboard reachable when it overflows on a phone. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="table-wrap" role="region" aria-label={props.caption ?? 'Generated table'} tabindex="0">
  <table>
    {#if props.caption}<caption>{props.caption}</caption>{/if}
    <thead><tr>{#each props.columns ?? [] as column, i (i)}<th scope="col">{column}</th>{/each}</tr></thead>
    <tbody>
      {#each props.rows ?? [] as row, i (i)}
        <tr>{#each props.columns ?? [] as column, j (`${i}-${j}`)}<td aria-label={`${column}: ${row?.[j] ?? ''}`}>{row?.[j] ?? ''}</td>{/each}</tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .table-wrap { max-width: 100%; overflow-x: auto; border: 1px solid var(--border); border-radius: var(--radius-sm); }
  .table-wrap:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
  caption { padding: 8px 10px; color: var(--text-2); text-align: left; font-size: 12px; }
  th, td { min-width: 92px; padding: 8px 10px; border-top: 1px solid var(--border); text-align: left; vertical-align: top; overflow-wrap: anywhere; }
  th { background: var(--surface-2); font-weight: 600; }
  td { background: var(--surface); }
</style>
