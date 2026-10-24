<script lang="ts">
  import { chartBar } from './motion';
  const readoutId = $props.id();
  let { days, series, scope = '' }: { days: { date: string; created: number; completed: number }[]; series: string; scope?: string } = $props();
  let selected = $state('');
  let pinned = $state(false);
  let container = $state<HTMLElement>();
  const day = $derived(days.find(d => d.date === selected));
  const max = $derived(Math.max(1, ...days.flatMap(d => series === 'created' ? [d.created] : series === 'completed' ? [d.completed] : [d.created, d.completed])));
  const step = $derived(640 / Math.max(1, days.length));
  function inspect(event: PointerEvent | MouseEvent) {
    const svg = container?.querySelector('svg');
    if (!svg) return;
    const bounds = svg.getBoundingClientRect();
    const index = Math.floor(((event.clientX-bounds.left)/bounds.width*700-40)/step);
    selected = days[Math.max(0, Math.min(days.length-1, index))]?.date ?? '';
  }
  function key(event: KeyboardEvent) {
    if (event.key === 'Escape') { selected = ''; pinned = false; return; }
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const previous = Math.max(0, days.findIndex(d => d.date === selected));
    const index = event.key === 'Home' ? 0 : event.key === 'End' ? days.length-1 : Math.max(0, Math.min(days.length-1, previous+(event.key === 'ArrowRight' || event.key === 'ArrowUp' ? 1 : -1)));
    selected = days[index]?.date ?? ''; pinned = true;
  }
</script>
<svelte:window onpointerdown={(event) => { if (container && event.target instanceof Node && !container.contains(event.target)) { selected = ''; pinned = false; } }} />
<div class="daily" bind:this={container} role="slider" aria-valuemin="0" aria-valuemax={Math.max(0, days.length-1)} aria-valuenow={Math.max(0, days.findIndex(d => d.date === selected))} aria-valuetext={day ? `${day.date}: ${day.created} created, ${day.completed} moved to Done` : days[0]?.date ?? 'No dates'} tabindex="0" aria-label="Daily activity chart. Use Left and Right arrows to inspect dates, or Escape to clear." aria-describedby={day ? readoutId : undefined} onkeydown={key} onpointermove={(event) => { if (event.pointerType === 'mouse' && !pinned) inspect(event); }} onpointerleave={() => { if (!pinned) selected = ''; }} onclick={(event) => { inspect(event); pinned = true; }}>
  <div class="legend"><span class="key created"></span>Created <span class="key completed"></span>Moved to Done <span class="unit">UTC days · peak {max} tasks</span></div>
  <svg viewBox="0 0 700 210" role="img" aria-label="Daily task counts. Hover a day, or use the arrow keys, to read it.">
    {#each (max > 1 && max % 2 === 0 ? [0, 0.5, 1] : [0, 1]) as ratio (ratio)}
      <line x1="40" x2="680" y1={170 - ratio * 144} y2={170 - ratio * 144} stroke="var(--border)" />
      <text x="30" y={174 - ratio * 144} text-anchor="end">{Math.round(max * ratio)}</text>
    {/each}
    {#each days as day, i (day.date)}
      <g><title>{day.date}: {day.created} created, {day.completed} moved to Done</title>
        {#if series !== 'completed'}<rect use:chartBar={{ value: day.created / max, vertical: true, scope: `${scope}/${series}` }} x={40 + i * step + step * .12} y={170 - day.created / max * 144} width={step * (series === 'both' ? .34 : .76)} height={day.created / max * 144} fill="var(--accent)" />{/if}
        {#if series !== 'created'}<rect use:chartBar={{ value: day.completed / max, vertical: true, scope: `${scope}/${series}` }} x={40 + i * step + step * (series === 'both' ? .52 : .12)} y={170 - day.completed / max * 144} width={step * (series === 'both' ? .34 : .76)} height={day.completed / max * 144} fill="var(--ok)" />{/if}
        {#if i % Math.max(1, Math.ceil(days.length / 6)) === 0 || i === days.length - 1}<text x={40 + (i + .5) * step} y="196" text-anchor="middle">{day.date.slice(5)}</text>{/if}
      </g>
    {/each}
  </svg>
  {#if day}<output class="readout" id={readoutId}>{day.date} · {day.created} created · {day.completed} moved to Done</output>{/if}
  <div class="mobile-dates"><span>{days[0]?.date.slice(5)}</span><span>{days[Math.floor(days.length / 2)]?.date.slice(5)}</span><span>{days[days.length - 1]?.date.slice(5)}</span></div>
</div>
<style>
  .daily { min-width: 0; position: relative; }
  .daily:focus-visible { outline: 2px solid var(--accent); outline-offset: 4px; }
  rect { transform-box: fill-box; transform-origin: center bottom; }
  .readout { position: absolute; top: 24px; right: 0; z-index: 1; max-width: 100%; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); box-shadow: var(--card-sh); font-size: 12px; pointer-events: none; animation: chart-readout 120ms ease-out; }
  @keyframes chart-readout { from { opacity: 0; } }
  .mobile-dates { display: none; }
  svg { display: block; width: 100%; min-height: 160px; max-height: 260px; }
  text { fill: var(--text-2); font: 12px var(--mono); }
  .legend { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 12px; color: var(--text-2); }
  .key { width: 10px; height: 10px; }
  .created { background: var(--accent); } .completed { background: var(--ok); }
  .unit { margin-left: auto; }
  @media (max-width: 600px) { text { display: none; } .mobile-dates { display: flex; justify-content: space-between; color: var(--text-2); font: 12px var(--mono); } }
</style>
