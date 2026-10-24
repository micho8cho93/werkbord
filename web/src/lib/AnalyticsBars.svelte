<script lang="ts">
  import { measuredNumber } from './analytics';
  import { chartBar } from './motion';
  export interface Bar { label: string; value: number | null; detail?: string; tone?: string; formatted?: string }
  let { rows, label, scope = '', shares = true, ceiling }: { rows: Bar[]; label: string; scope?: string; shares?: boolean; ceiling?: number } = $props();
  const max = $derived(Math.max(1, ceiling ?? Math.max(0, ...rows.map(row => row.value ?? 0))));
  const sum = $derived(rows.reduce((n, row) => n + (row.value ?? 0), 0));
</script>
<div class="comparison" role="group" aria-label={label}>
  {#each rows as row (row.label)}
    {@const tone = row.tone ?? 'var(--accent)'}
    {@const note = [row.detail, shares && row.value != null && sum > 0 && rows.length > 1 ? `${Math.round(row.value / sum * 100)}% of the total` : ''].filter(Boolean).join(' · ')}
    <div class="row" style:--tone={tone}>
      <div class="caption"><span><i class="sw" aria-hidden="true"></i>{row.label}</span><strong class="mm">{row.formatted ?? measuredNumber(row.value)}</strong></div>
      <div class="track" aria-hidden="true"><span class="fill" use:chartBar={{ value: (row.value ?? 0) / max, scope }} style:width={`${(row.value ?? 0) / max * 100}%`}></span></div>
      {#if note}<small>{note}</small>{/if}
    </div>
  {/each}
</div>
<style>
  .comparison { display: grid; gap: 14px; }
  .caption { display: flex; justify-content: space-between; gap: 16px; font-size: 13px; margin-bottom: 6px; }
  .caption span { display: inline-flex; align-items: center; gap: 8px; overflow-wrap: anywhere; }
  .sw { flex: none; width: 10px; height: 10px; border-radius: 3px; background: var(--tone); }
  strong { font-weight: 600; white-space: nowrap; }
  .track { height: 14px; padding: 2px; border-radius: 8px; background: var(--surface-2); box-shadow: var(--tray-sh); background-image: repeating-linear-gradient(90deg, transparent 0 calc(25% - 1px), color-mix(in srgb, var(--border) 80%, transparent) calc(25% - 1px) 25%); }
  .fill { display: block; height: 100%; min-width: 0; border-radius: 6px; background: linear-gradient(90deg, color-mix(in srgb, var(--tone) 58%, var(--surface)), var(--tone)); transform-origin: left center; }
  small { display: block; margin-top: 5px; color: var(--text-2); font-size: 12px; line-height: 1.5; }
</style>
