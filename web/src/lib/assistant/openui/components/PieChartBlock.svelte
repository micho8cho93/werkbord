<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ title?: string; categories?: string[]; values?: number[]; asOf?: string }> = $props();
  const colours = ['var(--accent)', 'var(--ok)', 'var(--warn)', 'var(--block)', 'var(--danger)'];
  const parts = $derived.by(() => {
    const rows = (props.categories ?? []).slice(0, 12).flatMap((label, i) => {
      const value = (props.values ?? [])[i];
      return Number.isFinite(value) && (value as number) > 0 ? [{ label, value: value as number }] : [];
    });
    const total = rows.reduce((sum, row) => sum + row.value, 0);
    let offset = 0;
    const circumference = 2 * Math.PI * 43;
    return rows.map((row, i) => {
      const length = total > 0 ? circumference * row.value / total : 0;
      const part = { ...row, colour: colours[i % colours.length], length, offset, percent: total > 0 ? row.value / total * 100 : 0 };
      offset += length;
      return part;
    });
  });
  const total = $derived(parts.reduce((sum, part) => sum + part.value, 0));
  const description = $derived(parts.map((part) => `${part.label}: ${part.value}`).join('; '));
</script>

<section class="chart">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if total <= 0}
    <p class="empty">No non-zero values are available in this snapshot.</p>
  {:else}
    <div class="pie-layout">
      <svg viewBox="0 0 120 120" role="img" aria-label={`${props.title ?? 'Pie chart'}. ${description}`}>
        <circle cx="60" cy="60" r="43" class="base" />
        {#each parts as part, i (`${part.label}-${i}`)}
          <circle cx="60" cy="60" r="43" fill="none" stroke={part.colour} stroke-width="18" stroke-dasharray={`${part.length} ${2 * Math.PI * 43 - part.length}`} stroke-dashoffset={-part.offset} transform="rotate(-90 60 60)" />
        {/each}
        <text x="60" y="58" class="total">{total}</text><text x="60" y="71" class="total-label">total</text>
      </svg>
      <ul class="legend">{#each parts as part, i (`${part.label}-${i}`)}
        <li><span class="swatch" style={`--swatch:${part.colour}`}></span><span class="label">{part.label}</span><strong>{part.value}</strong><span class="percent">{part.percent.toFixed(1)}%</span></li>
      {/each}</ul>
    </div>
  {/if}
</section>

<style>
  .chart { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .pie-layout { display: grid; grid-template-columns: minmax(110px, 150px) minmax(0, 1fr); align-items: center; gap: 16px; }
  svg { display: block; width: 100%; max-width: 150px; overflow: visible; }
  .base { fill: none; stroke: var(--surface-2); stroke-width: 18; }
  .total { fill: var(--text); text-anchor: middle; font: 600 16px var(--mono); font-variant-numeric: tabular-nums; }
  .total-label { fill: var(--text-2); text-anchor: middle; font: 10px var(--font); }
  .legend { display: grid; gap: 5px; list-style: none; margin: 0; padding: 0; }
  .legend li { display: grid; grid-template-columns: 9px minmax(0, 1fr) auto auto; align-items: baseline; gap: 7px; font-size: 12px; }
  .swatch { width: 9px; height: 9px; align-self: center; border-radius: 1px; background: var(--swatch); }
  .label { overflow-wrap: anywhere; }
  .legend strong, .percent { font-family: var(--mono); font-variant-numeric: tabular-nums; }
  .percent { min-width: 4.5ch; color: var(--text-2); text-align: right; }
  .empty { margin: 0; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-2); font-size: 12px; }
  @media (max-width: 420px) { .pie-layout { grid-template-columns: minmax(92px, 116px) minmax(0, 1fr); gap: 10px; } }
</style>
