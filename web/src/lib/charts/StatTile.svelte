<script lang="ts">
  import Spark from './Spark.svelte';
  // One figure, in a tile of its own colour, with its line over the period when it has one. Used inside a <dl>.
  let { label, value, hint = '', tone = 'var(--accent)', spark, sparkLabel = '' }: { label: string; value: string | number; hint?: string; tone?: string; spark?: number[]; sparkLabel?: string } = $props();
</script>

<div class="tile" style:--tone={tone}>
  <dt><span class="tone" aria-hidden="true"></span>{label}</dt>
  <dd>{value}</dd>
  {#if hint}<small>{hint}</small>{/if}
  {#if spark}<div class="line"><Spark values={spark} {tone} label={sparkLabel} /></div>{/if}
</div>

<style>
  .tile { position: relative; display: flex; flex-direction: column; min-width: 0; padding: 14px 16px 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); overflow: hidden; }
  dt { display: flex; align-items: center; gap: 8px; margin: 0; color: var(--text-2); font-size: 12px; font-weight: 500; }
  .tone { flex: none; width: 8px; height: 8px; border-radius: 50%; background: var(--tone); }
  dd { margin: 6px 0 0; font-size: 28px; font-weight: 600; font-variant-numeric: tabular-nums; letter-spacing: -0.02em; line-height: 1.15; overflow-wrap: anywhere; }
  :global(:root[data-style='classic']) .tile { border-color: color-mix(in srgb, var(--tone) 38%, var(--border)); border-radius: var(--radius-tray); background: linear-gradient(160deg, color-mix(in srgb, var(--tone) 11%, var(--surface)), var(--surface) 62%); }
  :global(:root[data-style='classic']) .tone { display: none; }
  :global(:root[data-style='classic']) dd { font-family: var(--mono); font-size: 26px; letter-spacing: 0; }
  small { margin-top: 2px; color: var(--text-2); font-size: 12px; line-height: 1.4; }
  .line { margin-top: auto; padding-top: 8px; }
</style>
