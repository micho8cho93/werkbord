<script lang="ts">
  import { niceAxis } from './analytics';
  import { chartBar } from './motion';
  const readoutId = $props.id();
  let { days, series, scope = '' }: { days: { date: string; created: number; completed: number }[]; series: string; scope?: string } = $props();
  const H = 230, L = 34, R = 8, T = 14, B = 26;
  let selected = $state('');
  let pinned = $state(false);
  let container = $state<HTMLElement>();
  let width = $state(640);
  const day = $derived(days.find(d => d.date === selected));
  const axis = $derived(niceAxis(Math.max(1, ...days.flatMap(d => series === 'created' ? [d.created] : series === 'completed' ? [d.completed] : [d.created, d.completed]))));
  const step = $derived((width - L - R) / Math.max(1, days.length));
  const base = H - B;
  const h = (v: number) => (v / axis.top) * (base - T);
  const barW = $derived(Math.max(2, step * (series === 'both' ? .36 : .72) - 1));
  const peak = $derived(Math.max(0, ...days.flatMap(d => series === 'created' ? [d.created] : series === 'completed' ? [d.completed] : [d.created, d.completed])));
  function inspect(event: PointerEvent | MouseEvent) {
    const svg = container?.querySelector('svg');
    if (!svg) return;
    const bounds = svg.getBoundingClientRect();
    const index = Math.floor((event.clientX - bounds.left - L) / step);
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
<div class="daily" bind:this={container} bind:clientWidth={width} role="slider" aria-valuemin="0" aria-valuemax={Math.max(0, days.length-1)} aria-valuenow={Math.max(0, days.findIndex(d => d.date === selected))} aria-valuetext={day ? `${day.date}: ${day.created} created, ${day.completed} moved to Done` : days[0]?.date ?? 'No dates'} tabindex="0" aria-label="Daily activity chart. Use Left and Right arrows to inspect dates, or Escape to clear." aria-describedby={day ? readoutId : undefined} onkeydown={key} onpointermove={(event) => { if (event.pointerType === 'mouse' && !pinned) inspect(event); }} onpointerleave={() => { if (!pinned) selected = ''; }} onclick={(event) => { inspect(event); pinned = true; }}>
  <div class="legend"><span class="key created"></span>Created <span class="key completed"></span>Moved to Done <span class="unit">UTC days · peak {peak} {peak === 1 ? 'task' : 'tasks'}</span></div>
  <svg {width} height={H} viewBox="0 0 {width} {H}" role="img" aria-label="Daily task counts. Hover a day, or use the arrow keys, to read it.">
    <defs>
      <linearGradient id="{readoutId}-c" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stop-color="var(--accent)" /><stop offset="1" stop-color="color-mix(in srgb, var(--accent) 62%, var(--surface))" /></linearGradient>
      <linearGradient id="{readoutId}-d" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stop-color="var(--ok)" /><stop offset="1" stop-color="color-mix(in srgb, var(--ok) 62%, var(--surface))" /></linearGradient>
    </defs>
    {#each axis.ticks as t (t)}
      <line x1={L} x2={width - R} y1={base - h(t)} y2={base - h(t)} stroke="var(--border)" stroke-dasharray={t === 0 ? undefined : '2 4'} />
      <text x={L - 8} y={base - h(t) + 4} text-anchor="end">{t}</text>
    {/each}
    {#if day}<rect class="focus" x={L + days.indexOf(day) * step} y={T} width={step} height={base - T} />{/if}
    {#each days as d, i (d.date)}
      <g><title>{d.date}: {d.created} created, {d.completed} moved to Done</title>
        {#if series !== 'completed'}<rect use:chartBar={{ value: d.created / axis.top, vertical: true, scope: `${scope}/${series}` }} x={L + i * step + (series === 'both' ? step * .12 : step * .14)} y={base - h(d.created)} width={barW} height={h(d.created)} rx={Math.min(3, barW / 2)} fill="url(#{readoutId}-c)" />{/if}
        {#if series !== 'created'}<rect use:chartBar={{ value: d.completed / axis.top, vertical: true, scope: `${scope}/${series}` }} x={L + i * step + (series === 'both' ? step * .52 : step * .14)} y={base - h(d.completed)} width={barW} height={h(d.completed)} rx={Math.min(3, barW / 2)} fill="url(#{readoutId}-d)" />{/if}
        {#if i === days.length - 1 || (i % Math.max(1, Math.ceil(days.length / Math.max(2, Math.floor((width - L - R) / 56)))) === 0 && (days.length - 1 - i) * step >= 56)}<text x={L + (i + .5) * step} y={H - 7} text-anchor="middle">{d.date.slice(5)}</text>{/if}
      </g>
    {/each}
  </svg>
  {#if day}<output class="readout" id={readoutId}>{day.date} · {day.created} created · {day.completed} moved to Done</output>{/if}
</div>
<style>
  .daily { min-width: 0; position: relative; }
  .daily:focus-visible { outline: 2px solid var(--accent); outline-offset: 4px; }
  rect { transform-box: fill-box; transform-origin: center bottom; }
  .focus { fill: color-mix(in srgb, var(--text) 7%, transparent); }
  .readout { position: absolute; top: 24px; right: 0; z-index: 1; max-width: 100%; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); box-shadow: var(--card-sh); font-size: 12px; pointer-events: none; animation: chart-readout 120ms ease-out; }
  @keyframes chart-readout { from { opacity: 0; } }
  svg { display: block; max-width: 100%; overflow: visible; }
  text { fill: var(--text-2); font: 11.5px var(--mono); }
  .legend { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 8px; font-size: 12px; color: var(--text-2); }
  .key { width: 10px; height: 10px; border-radius: 3px; }
  .created { background: var(--accent); } .completed { background: var(--ok); }
  .unit { margin-left: auto; }
</style>
