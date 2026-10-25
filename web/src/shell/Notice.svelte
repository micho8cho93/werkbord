<script lang="ts">
  import type { Snippet } from 'svelte';

  // A line a page says about itself rather than about the work: a workspace that could not answer, so its figures are left out.
  let { children }: { children: Snippet } = $props();
</script>

<p class="notice" role="status">{@render children()}</p>

<style>
  .notice { margin: 0; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface-2); font-size: 13px; }
</style>
