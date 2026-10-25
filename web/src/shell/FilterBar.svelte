<script lang="ts">
  import type { Snippet } from 'svelte';

  // The row of choices under a page's title: where to look, for how long, which kind. A choice that is a segmented control goes last
  // and is pushed to the end of the row.
  let { busy = false, children }: { busy?: boolean; children: Snippet } = $props();
</script>

<div class="filters" aria-busy={busy}>{@render children()}</div>

<style>
  .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-tray, var(--radius)); background: var(--surface); }
  .filters > :global(.seg) { margin-left: auto; }
  @media (max-width: 899px) { .filters > :global(.seg) { margin-left: 0; } }
</style>
