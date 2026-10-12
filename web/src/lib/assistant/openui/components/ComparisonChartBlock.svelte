<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ title?: string; categories?: string[]; firstLabel?: string; firstValues?: number[]; secondLabel?: string; secondValues?: number[]; unit?: string; note?: string; asOf?: string }> = $props();
  const rows = $derived((props.categories ?? []).slice(0, 16).flatMap((category, i) => {
    const first = (props.firstValues ?? [])[i];
    const second = (props.secondValues ?? [])[i];
    return Number.isFinite(first) && Number.isFinite(second) ? [{ category, first: first as number, second: second as number }] : [];
  }));
  const peak = $derived(Math.max(0, ...rows.flatMap((row) => [row.first, row.second])));
</script>

<section class="chart">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if rows.length === 0}
    <p class="empty">No paired values are available in this snapshot.</p>
  {:else}
    <div class="legend"><span class="key first"></span>{props.firstLabel ?? 'First'}<span class="key second"></span>{props.secondLabel ?? 'Second'}</div>
    <div class="rows" role="img" aria-label={`${props.title ?? 'Comparison chart'} comparing ${props.firstLabel ?? 'First'} with ${props.secondLabel ?? 'Second'}`}>
      {#each rows as row, i (`${row.category}-${i}`)}
        <div class="group">
          <span class="category">{row.category}</span>
          <div class="pair">
            <div class="measure"><span class="track"><span class="bar first" style={`width:${peak ? Math.max(0, row.first / peak * 100) : 0}%`}></span></span><span class="value">{row.first}{props.unit ? ` ${props.unit}` : ''}</span></div>
            <div class="measure"><span class="track"><span class="bar second" style={`width:${peak ? Math.max(0, row.second / peak * 100) : 0}%`}></span></span><span class="value">{row.second}{props.unit ? ` ${props.unit}` : ''}</span></div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
  {#if props.note}<p class="note">{props.note}</p>{/if}
</section>

<style>
  .chart { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .legend { display: flex; flex-wrap: wrap; align-items: center; gap: 5px 8px; color: var(--text-2); font-size: 11.5px; }
  .key { width: 10px; height: 10px; border-radius: 1px; }
  .key.first { background: var(--accent); }
  .key.second { margin-left: 8px; background: var(--ok); }
  .rows { display: grid; gap: 10px; }
  .group { display: grid; grid-template-columns: minmax(72px, .8fr) minmax(120px, 2fr); align-items: center; gap: 10px; }
  .category { font-size: 12px; overflow-wrap: anywhere; }
  .pair { display: grid; gap: 4px; min-width: 0; }
  .measure { display: grid; grid-template-columns: minmax(30px, 1fr) auto; align-items: center; gap: 7px; }
  .track { display: block; height: 8px; min-width: 0; overflow: hidden; border-radius: 2px; background: var(--surface-2); }
  .bar { display: block; height: 100%; border-radius: inherit; }
  .bar.first { background: var(--accent); }
  .bar.second { background: var(--ok); }
  .value { min-width: 2ch; color: var(--text-2); text-align: right; font: 11.5px var(--mono); font-variant-numeric: tabular-nums; }
  .note, .empty { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.45; }
  .empty { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); }
</style>
