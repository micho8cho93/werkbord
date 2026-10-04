<script lang="ts">
  import type { Chip } from '../gitui';

  let { chips }: { chips: Chip[] } = $props();
</script>

{#each chips as c (c.text)}<span class="g-chip" data-tone={c.tone}>{c.text}</span>{/each}
