<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ title?: string; categories?: string[]; values?: number[]; unit?: string; note?: string; asOf?: string }> = $props();
  const rows = $derived((props.categories ?? []).slice(0, 20).flatMap((category, i) => {
    const value = (props.values ?? [])[i];
    return Number.isFinite(value) ? [{ category, value: value as number }] : [];
  }));
  const peak = $derived(Math.max(0, ...rows.map((row) => row.value)));
  const description = $derived(rows.map((row) => `${row.category}: ${row.value}${props.unit ? ` ${props.unit}` : ''}`).join('; '));
</script>

<section class="chart">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if rows.length === 0}
    <p class="empty">No chart values are available in this snapshot.</p>
  {:else}
    <div class="bars" role="img" aria-label={`${props.title ?? 'Bar chart'}. ${description}`}>
      {#each rows as row, i (`${row.category}-${i}`)}
        {@const width = peak > 0 ? Math.max(0, row.value / peak * 100) : 0}
        <div class="bar-row" title={`${row.category}: ${row.value}${props.unit ? ` ${props.unit}` : ''}`}>
          <span class="category">{row.category}</span>
          <span class="track"><span class="bar" style={`width:${width}%`}></span></span>
          <span class="value">{row.value}{props.unit ? ` ${props.unit}` : ''}</span>
        </div>
      {/each}
    </div>
  {/if}
  {#if props.note}<p class="note">{props.note}</p>{/if}
</section>

<style>
  .chart { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .bars { display: grid; gap: 7px; min-width: 0; }
  .bar-row { display: grid; grid-template-columns: minmax(68px, 1fr) minmax(54px, 2fr) auto; align-items: center; gap: 8px; min-width: 0; }
  .category { overflow-wrap: anywhere; font-size: 12px; }
  .track { display: block; height: 10px; min-width: 0; overflow: hidden; border-radius: 2px; background: var(--surface-2); }
  .bar { display: block; height: 100%; min-width: 0; border-radius: inherit; background: var(--accent); }
  .value { min-width: 2ch; color: var(--text-2); text-align: right; font: 12px var(--mono); font-variant-numeric: tabular-nums; }
  .note, .empty { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.45; }
  .empty { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); }
</style>
