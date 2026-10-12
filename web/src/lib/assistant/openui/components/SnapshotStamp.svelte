<script lang="ts">
  let { asOf }: { asOf?: string } = $props();
</script>

{#if asOf}
  <p class="stamp"><span>Snapshot</span><time>{asOf}</time></p>
{/if}

<style>
  .stamp { display: flex; flex-wrap: wrap; gap: 4px 8px; margin: 0; color: var(--text-2); font-size: 11.5px; line-height: 1.4; }
  time { font-family: var(--mono); font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
</style>
