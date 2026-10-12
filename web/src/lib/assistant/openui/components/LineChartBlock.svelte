<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ title?: string; labels?: string[]; values?: number[]; unit?: string; note?: string; asOf?: string }> = $props();
  const points = $derived.by(() => {
    const labels = props.labels ?? [];
    const values = props.values ?? [];
    const count = Math.min(91, labels.length, values.length);
    const rows = [];
    for (let i = 0; i < count; i++) if (Number.isFinite(values[i])) rows.push({ label: labels[i], value: values[i] });
    if (rows.length === 0) return [];
    const low = Math.min(0, ...rows.map((point) => point.value));
    const high = Math.max(0, ...rows.map((point) => point.value));
    const span = high === low ? 1 : high - low;
    return rows.map((point, i) => ({ ...point, x: rows.length === 1 ? 300 : 12 + i * 576 / (rows.length - 1), y: 136 - (point.value - low) / span * 120 }));
  });
  const polyline = $derived(points.map((point) => `${point.x},${point.y}`).join(' '));
  const description = $derived(points.map((point) => `${point.label}: ${point.value}${props.unit ? ` ${props.unit}` : ''}`).join('; '));
</script>

<section class="chart">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if points.length === 0}
    <p class="empty">No dated values are available in this snapshot.</p>
  {:else}
    <div class="plot" role="img" aria-label={`${props.title ?? 'Line chart'}. ${description}`}>
      <svg viewBox="0 0 600 168" preserveAspectRatio="none" aria-hidden="true">
        <line x1="12" y1="16" x2="588" y2="16" class="guide" />
        <line x1="12" y1="76" x2="588" y2="76" class="guide" />
        <line x1="12" y1="136" x2="588" y2="136" class="axis" />
        {#if points.length > 1}<polyline points={polyline} class="line" />{/if}
        {#each points as point, i (`${point.label}-${i}`)}
          <circle cx={point.x} cy={point.y} r="3.5" class="point"><title>{point.label}: {point.value}{props.unit ? ` ${props.unit}` : ''}</title></circle>
        {/each}
      </svg>
      <div class="axis-labels"><span>{points[0].label}</span>{#if points.length > 1}<span>{points[points.length - 1].label}</span>{/if}</div>
    </div>
    <details class="data">
      <summary>View {points.length} recorded values</summary>
      <ul>{#each points as point, i (`${point.label}-${i}`)}<li><span>{point.label}</span><strong>{point.value}{props.unit ? ` ${props.unit}` : ''}</strong></li>{/each}</ul>
    </details>
  {/if}
  {#if props.note}<p class="note">{props.note}</p>{/if}
</section>

<style>
  .chart { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .plot { min-width: 0; padding: 8px 10px 0; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  svg { display: block; width: 100%; height: clamp(120px, 28vw, 180px); overflow: visible; }
  .guide { stroke: var(--border); stroke-width: 1; stroke-dasharray: 3 4; }
  .axis { stroke: var(--border); stroke-width: 1; }
  .line { fill: none; stroke: var(--accent); stroke-width: 3; stroke-linecap: round; stroke-linejoin: round; }
  .point { fill: var(--surface); stroke: var(--accent); stroke-width: 2.5; }
  .axis-labels { display: flex; justify-content: space-between; gap: 8px; color: var(--text-2); font: 11.5px var(--mono); }
  .axis-labels span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .data summary { width: fit-content; min-height: 36px; display: flex; align-items: center; cursor: pointer; color: var(--text-2); font-size: 12px; }
  .data ul { max-height: 220px; overflow: auto; list-style: none; margin: 0; padding: 0; border-top: 1px solid var(--border); }
  .data li { display: flex; justify-content: space-between; gap: 12px; padding: 5px 2px; border-bottom: 1px solid var(--border); font-size: 12px; }
  .data strong { font-family: var(--mono); font-variant-numeric: tabular-nums; }
  .note, .empty { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.45; }
  .empty { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); }
</style>
