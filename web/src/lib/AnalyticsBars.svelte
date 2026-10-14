<script lang="ts">
  import { measuredNumber } from './analytics';
  export interface Bar { label: string; value: number | null; detail?: string; tone?: string; formatted?: string }
  let { rows, label }: { rows: Bar[]; label: string } = $props();
  const max = $derived(Math.max(0, ...rows.map(row => row.value ?? 0)) || 1);
</script>
<div class="comparison" role="group" aria-label={label}>
  {#each rows as row (row.label)}
    <div class="row">
      <div class="caption"><span>{row.label}</span><strong class="mm">{row.formatted ?? measuredNumber(row.value)}</strong></div>
      <div class="track" aria-hidden="true"><span style:width={`${(row.value ?? 0) / max * 100}%`} style:background={row.tone ?? 'var(--accent)'}></span></div>
      {#if row.detail}<small>{row.detail}</small>{/if}
    </div>
  {/each}
</div>
<style>
  .comparison { display: grid; gap: 18px; }
  .caption { display: flex; justify-content: space-between; gap: 16px; font-size: 13px; margin-bottom: 7px; }
  .caption span { overflow-wrap: anywhere; }
  strong { font-weight: 500; white-space: nowrap; }
  .track { height: 10px; background: var(--surface-2); border-radius: 2px; overflow: hidden; }
  .track span { display: block; height: 100%; min-width: 0; }
  small { display: block; margin-top: 5px; color: var(--text-2); font-size: 12px; line-height: 1.5; }
</style>
