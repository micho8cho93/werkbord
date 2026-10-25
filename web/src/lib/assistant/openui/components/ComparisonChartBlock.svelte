<script lang="ts">
  import BlockEmpty from './BlockEmpty.svelte';
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ title?: string; categories?: string[]; firstLabel?: string; firstValues?: number[]; secondLabel?: string; secondValues?: number[]; unit?: string; note?: string; asOf?: string }> = $props();
  let selectedCategory = $state('');
  let showFirst = $state(true);
  let showSecond = $state(true);
  const rows = $derived((props.categories ?? []).slice(0, 16).flatMap((category, i) => {
    const first = (props.firstValues ?? [])[i];
    const second = (props.secondValues ?? [])[i];
    return Number.isFinite(first) && Number.isFinite(second) ? [{ category, first: first as number, second: second as number }] : [];
  }));
  const peak = $derived(Math.max(0, ...rows.flatMap((row) => [row.first, row.second])));
  const visibleRows = $derived(rows.filter((row) => !selectedCategory || row.category === selectedCategory));
  const firstTotal = $derived(visibleRows.reduce((sum, row) => sum + row.first, 0));
  const secondTotal = $derived(visibleRows.reduce((sum, row) => sum + row.second, 0));
</script>

<section class="chart">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if rows.length === 0}
    <BlockEmpty>No paired values are available in this snapshot.</BlockEmpty>
  {:else}
    <div class="controls">
      <label class="criterion"><span>Category</span><select aria-label="Compare category" bind:value={selectedCategory}><option value="">All categories</option>{#each rows as row, i (`${row.category}-${i}`)}<option value={row.category}>{row.category}</option>{/each}</select></label>
      <div class="series" role="group" aria-label="Series to compare">
        <label><input type="checkbox" bind:checked={showFirst} /><span class="key first"></span>{props.firstLabel ?? 'First'}</label>
        <label><input type="checkbox" bind:checked={showSecond} /><span class="key second"></span>{props.secondLabel ?? 'Second'}</label>
      </div>
    </div>
    {#if showFirst || showSecond}
    <div class="rows" role="list" aria-label={`${props.title ?? 'Comparison chart'} values`}>
      {#each visibleRows as row, i (`${row.category}-${i}`)}
        <div class="group">
          <span class="category">{row.category}</span>
          <div class="pair">
            {#if showFirst}<div class="measure"><span class="series-name">{props.firstLabel ?? 'First'}</span><span class="track"><span class="bar first" style={`width:${peak ? Math.max(0, row.first / peak * 100) : 0}%`}></span></span><span class="value">{row.first}{props.unit ? ` ${props.unit}` : ''}</span></div>{/if}
            {#if showSecond}<div class="measure"><span class="series-name">{props.secondLabel ?? 'Second'}</span><span class="track"><span class="bar second" style={`width:${peak ? Math.max(0, row.second / peak * 100) : 0}%`}></span></span><span class="value">{row.second}{props.unit ? ` ${props.unit}` : ''}</span></div>{/if}
          </div>
        </div>
      {/each}
    </div>
    <p class="totals" role="status">Totals{selectedCategory ? ` for ${selectedCategory}` : ''}: {#if showFirst}<span>{props.firstLabel ?? 'First'} {firstTotal}{props.unit ? ` ${props.unit}` : ''}</span>{/if}{#if showFirst && showSecond}<span aria-hidden="true"> · </span>{/if}{#if showSecond}<span>{props.secondLabel ?? 'Second'} {secondTotal}{props.unit ? ` ${props.unit}` : ''}</span>{/if}</p>
    {:else}<BlockEmpty>Select at least one series to compare.</BlockEmpty>{/if}
  {/if}
  {#if props.note}<p class="note">{props.note}</p>{/if}
</section>

<style>
  .chart { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .controls { display: flex; flex-wrap: wrap; align-items: end; gap: 10px 18px; }
  .criterion { display: grid; gap: 4px; color: var(--text-2); font-size: 12px; }
  select { min-width: 150px; min-height: 36px; padding: 5px 28px 5px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; }
  select:focus-visible, input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .series { display: flex; flex-wrap: wrap; gap: 6px 12px; }
  .series label { display: inline-flex; min-height: 36px; align-items: center; gap: 6px; color: var(--text-2); font-size: 12px; cursor: pointer; }
  .series input { width: 15px; height: 15px; accent-color: var(--accent); }
  .key { width: 10px; height: 10px; border-radius: 1px; }
  .key.first { background: var(--accent); }
  .key.second { background: var(--ok); }
  .rows { display: grid; gap: 10px; }
  .group { display: grid; grid-template-columns: minmax(72px, .8fr) minmax(120px, 2fr); align-items: center; gap: 10px; }
  .category { font-size: 12px; overflow-wrap: anywhere; }
  .pair { display: grid; gap: 4px; min-width: 0; }
  .measure { display: grid; grid-template-columns: minmax(48px, .7fr) minmax(30px, 1fr) auto; align-items: center; gap: 7px; }
  .series-name { overflow-wrap: anywhere; color: var(--text-2); font-size: 12px; }
  .track { display: block; height: 8px; min-width: 0; overflow: hidden; border-radius: 2px; background: var(--surface-2); }
  .bar { display: block; height: 100%; border-radius: inherit; }
  .bar.first { background: var(--accent); }
  .bar.second { background: var(--ok); }
  .value { min-width: 2ch; color: var(--text-2); text-align: right; font: 12px var(--mono); font-variant-numeric: tabular-nums; }
  .note, .totals { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.45; }
  .totals span { color: var(--text); font-family: var(--mono); font-variant-numeric: tabular-nums; }
  @media (max-width: 600px) {
    select { width: 100%; min-height: 44px; font-size: 16px; }
    .criterion { width: 100%; }
    .series label { min-height: 44px; }
    .group { grid-template-columns: minmax(65px, .7fr) minmax(0, 2fr); }
    .measure { grid-template-columns: minmax(42px, .7fr) minmax(24px, 1fr) auto; gap: 5px; }
  }
</style>
