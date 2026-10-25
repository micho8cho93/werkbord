<script lang="ts">
  import type { Snippet } from 'svelte';

  // What a block of the assistant's says when there is nothing to draw: one quiet boxed line, the same in every block.
  let { children }: { children: Snippet } = $props();
</script>

<p class="empty">{@render children()}</p>

<style>
  .empty { margin: 0; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-2); font-size: 12px; line-height: 1.45; }
</style>
