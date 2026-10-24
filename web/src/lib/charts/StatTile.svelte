<script lang="ts">
  import Spark from './Spark.svelte';
  // One figure, in a tile of its own colour, with its line over the period when it has one. Used inside a <dl>.
  let { label, value, hint = '', tone = 'var(--accent)', spark, sparkLabel = '' }: { label: string; value: string | number; hint?: string; tone?: string; spark?: number[]; sparkLabel?: string } = $props();
</script>

<div class="tile" style:--tone={tone}>
  <dt>{label}</dt>
  <dd>{value}</dd>
  {#if hint}<small>{hint}</small>{/if}
  {#if spark}<div class="line"><Spark values={spark} {tone} label={sparkLabel} /></div>{/if}
</div>

<style>
  .tile { position: relative; display: flex; flex-direction: column; min-width: 0; padding: 14px 16px 12px; border: 1px solid color-mix(in srgb, var(--tone) 38%, var(--border)); border-radius: var(--radius-tray); background: linear-gradient(160deg, color-mix(in srgb, var(--tone) 11%, var(--surface)), var(--surface) 62%); box-shadow: var(--card-sh); overflow: hidden; }
  dt { margin: 0; color: var(--text-2); font-size: 12px; font-weight: 500; }
  dd { margin: 4px 0 0; font-family: var(--mono); font-size: 26px; font-weight: 600; font-variant-numeric: tabular-nums; line-height: 1.15; overflow-wrap: anywhere; }
  small { margin-top: 2px; color: var(--text-2); font-size: 12px; line-height: 1.4; }
  .line { margin-top: auto; padding-top: 8px; }
</style>
