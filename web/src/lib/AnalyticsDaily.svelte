<script lang="ts">
  let { days, series }: { days: { date: string; created: number; completed: number }[]; series: string } = $props();
  const max = $derived(Math.max(1, ...days.flatMap(d => series === 'created' ? [d.created] : series === 'completed' ? [d.completed] : [d.created, d.completed])));
  const step = $derived(640 / Math.max(1, days.length));
</script>
<div class="daily">
  <div class="legend"><span class="key created"></span>Created <span class="key completed"></span>Moved to Done <span class="unit">UTC days · peak {max} tasks</span></div>
  <svg viewBox="0 0 700 210" role="img" aria-label="Daily task counts. Exact values are in Daily values below.">
    {#each (max > 1 && max % 2 === 0 ? [0, 0.5, 1] : [0, 1]) as ratio (ratio)}
      <line x1="40" x2="680" y1={170 - ratio * 144} y2={170 - ratio * 144} stroke="var(--border)" />
      <text x="30" y={174 - ratio * 144} text-anchor="end">{Math.round(max * ratio)}</text>
    {/each}
    {#each days as day, i (day.date)}
      <g><title>{day.date}: {day.created} created, {day.completed} moved to Done</title>
        {#if series !== 'completed'}<rect x={40 + i * step + step * .12} y={170 - day.created / max * 144} width={step * (series === 'both' ? .34 : .76)} height={day.created / max * 144} fill="var(--accent)" />{/if}
        {#if series !== 'created'}<rect x={40 + i * step + step * (series === 'both' ? .52 : .12)} y={170 - day.completed / max * 144} width={step * (series === 'both' ? .34 : .76)} height={day.completed / max * 144} fill="var(--ok)" />{/if}
        {#if i % Math.max(1, Math.ceil(days.length / 6)) === 0 || i === days.length - 1}<text x={40 + (i + .5) * step} y="196" text-anchor="middle">{day.date.slice(5)}</text>{/if}
      </g>
    {/each}
  </svg>
  <div class="mobile-dates"><span>{days[0]?.date.slice(5)}</span><span>{days[Math.floor(days.length / 2)]?.date.slice(5)}</span><span>{days[days.length - 1]?.date.slice(5)}</span></div>
</div>
<style>
  .daily { min-width: 0; }
  .mobile-dates { display: none; }
  svg { display: block; width: 100%; min-height: 160px; max-height: 260px; }
  text { fill: var(--text-2); font: 12px var(--mono); }
  .legend { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 12px; color: var(--text-2); }
  .key { width: 10px; height: 10px; }
  .created { background: var(--accent); } .completed { background: var(--ok); }
  .unit { margin-left: auto; }
  @media (max-width: 600px) { text { display: none; } .mobile-dates { display: flex; justify-content: space-between; color: var(--text-2); font: 12px var(--mono); } }
</style>
