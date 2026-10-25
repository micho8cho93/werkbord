<script lang="ts">
  import BlockEmpty from './BlockEmpty.svelte';
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ title?: string; categories?: string[]; values?: number[]; unit?: string; note?: string; asOf?: string }> = $props();
  let selectedCategory = $state<number | null>(null);
  const rows = $derived((props.categories ?? []).slice(0, 20).flatMap((category, i) => {
    const value = (props.values ?? [])[i];
    return Number.isFinite(value) ? [{ category, value: value as number }] : [];
  }));
  const peak = $derived(Math.max(0, ...rows.map((row) => row.value)));
  const description = $derived(rows.map((row) => `${row.category}: ${row.value}${props.unit ? ` ${props.unit}` : ''}`).join('; '));
  const selected = $derived(selectedCategory === null ? null : rows[selectedCategory] ?? null);
</script>

<section class="chart">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if rows.length === 0}
    <BlockEmpty>No chart values are available in this snapshot.</BlockEmpty>
  {:else}
    <div class="bars" role="group" aria-label={`${props.title ?? 'Bar chart'}. Select a category to inspect its value. ${description}`}>
      {#each rows as row, i (`${row.category}-${i}`)}
        {@const width = peak > 0 ? Math.max(0, row.value / peak * 100) : 0}
        <button class="bar-row" type="button" aria-pressed={selectedCategory === i} onclick={() => (selectedCategory = selectedCategory === i ? null : i)} title={`${row.category}: ${row.value}${props.unit ? ` ${props.unit}` : ''}`}>
          <span class="category">{row.category}</span>
          <span class="track"><span class="bar" style={`width:${width}%`}></span></span>
          <span class="value">{row.value}{props.unit ? ` ${props.unit}` : ''}</span>
        </button>
      {/each}
    </div>
    {#if selected}<p class="selection" role="status">Selected: <strong>{selected.category}</strong> · {selected.value}{props.unit ? ` ${props.unit}` : ''}</p>{/if}
  {/if}
  {#if props.note}<p class="note">{props.note}</p>{/if}
</section>

<style>
  .chart { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .bars { display: grid; gap: 7px; min-width: 0; }
  .bar-row { display: grid; grid-template-columns: minmax(68px, 1fr) minmax(54px, 2fr) auto; align-items: center; gap: 8px; min-width: 0; min-height: 36px; padding: 2px 4px; border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--text); text-align: left; font: inherit; cursor: pointer; }
  .bar-row:hover, .bar-row[aria-pressed='true'] { background: var(--surface-2); }
  .bar-row[aria-pressed='true'] { border-color: var(--border); }
  .bar-row:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .category { overflow-wrap: anywhere; font-size: 12px; }
  .track { display: block; height: 10px; min-width: 0; overflow: hidden; border-radius: 2px; background: var(--surface-2); }
  .bar { display: block; height: 100%; min-width: 0; border-radius: inherit; background: var(--accent); }
  .value { min-width: 2ch; color: var(--text-2); text-align: right; font: 12px var(--mono); font-variant-numeric: tabular-nums; }
  .note, .selection { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.45; }
  .selection strong { color: var(--text); font-weight: 600; }
  @media (max-width: 600px) { .bar-row { min-height: 44px; grid-template-columns: minmax(62px, 1fr) minmax(44px, 2fr) auto; } }
</style>
