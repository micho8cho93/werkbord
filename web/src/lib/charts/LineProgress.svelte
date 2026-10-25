<script lang="ts">
  import { cumulative, niceAxis, smoothPath } from '../analytics';
  import { chartDraw, chartGrow } from '../motion';
  // Progress over the period as two running totals: everything created, and everything moved to Done. The band between them is the
  // difference between the period’s two totals; it is not the outstanding backlog. Hover a day, or use the arrow keys, to read it.
  let { days, series = 'both' }: { days: { date: string; created: number; completed: number }[]; series?: string } = $props();
  const readoutId = $props.id();
  const H = 250, L = 38, R = 64, T = 16, B = 28;
  let width = $state(640);
  let selected = $state(-1);
  const runs = $derived(cumulative(days));
  const showCreated = $derived(series !== 'completed');
  const showDone = $derived(series !== 'created');
  const axis = $derived(niceAxis(Math.max(0, ...runs.flatMap((d) => [showCreated ? d.created : 0, showDone ? d.completed : 0]))));
  const x = (i: number) => L + (runs.length > 1 ? (i * (width - L - R)) / (runs.length - 1) : (width - L - R) / 2);
  const y = (v: number) => T + (1 - v / axis.top) * (H - T - B);
  const pc = $derived(runs.map((d, i) => [x(i), y(d.created)] as [number, number]));
  const pd = $derived(runs.map((d, i) => [x(i), y(d.completed)] as [number, number]));
  const lineC = $derived(smoothPath(pc));
  const lineD = $derived(smoothPath(pd));
  const base = $derived(y(0));
  const areaC = $derived(runs.length ? `${lineC} L${x(runs.length - 1)},${base} L${x(0)},${base} Z` : '');
  const areaD = $derived(runs.length ? `${lineD} L${x(runs.length - 1)},${base} L${x(0)},${base} Z` : '');
  const gap = $derived(runs.length ? `${lineC} ${smoothPath([...pd].reverse()).replace(/^M/, 'L')} Z` : '');
  const last = $derived(runs[runs.length - 1]);
  const picked = $derived(runs[selected]);
  const labelEvery = $derived(Math.max(1, Math.ceil(runs.length / Math.max(2, Math.floor((width - L - R) / 64)))));

  function index(event: PointerEvent | MouseEvent, el: Element): number {
    const box = el.getBoundingClientRect();
    return Math.max(0, Math.min(runs.length - 1, Math.round(((event.clientX - box.left - L) / Math.max(1, width - L - R)) * (runs.length - 1))));
  }
  function key(event: KeyboardEvent) {
    if (event.key === 'Escape') { selected = -1; return; }
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const at = selected < 0 ? runs.length - 1 : selected;
    selected = event.key === 'Home' ? 0 : event.key === 'End' ? runs.length - 1 : Math.max(0, Math.min(runs.length - 1, at + (event.key === 'ArrowRight' ? 1 : -1)));
  }
</script>

<div class="lp" bind:clientWidth={width}>
  <div class="legend">
    {#if showCreated}<span><i class="k created"></i>Created so far</span>{/if}
    {#if showDone}<span><i class="k done"></i>Moved to Done so far</span>{/if}
    {#if showCreated && showDone}<span><i class="k open"></i>Difference</span>{/if}
  </div>
  <div class="plot" role="slider" tabindex="0" aria-label="Progress over time. Use Left and Right arrows to read each day, or Escape to clear." aria-valuemin="0" aria-valuemax={Math.max(0, runs.length - 1)} aria-valuenow={Math.max(0, selected)} aria-valuetext={picked ? `${picked.date}: ${picked.created} created, ${picked.completed} moved to Done so far` : 'No day selected'} aria-describedby={picked ? readoutId : undefined} onkeydown={key} onpointermove={(e) => { if (e.pointerType === 'mouse') selected = index(e, e.currentTarget); }} onpointerleave={() => (selected = -1)} onclick={(e) => (selected = index(e, e.currentTarget))}>
    <svg {width} height={H} viewBox="0 0 {width} {H}" role="img" aria-label="Running totals of tasks created and moved to Done">
      {#each axis.ticks as t (t)}
        <line class="grid" x1={L} x2={width - R} y1={y(t)} y2={y(t)} />
        <text class="tick" x={L - 8} y={y(t) + 4} text-anchor="end">{t}</text>
      {/each}
      {#each runs as d, i (d.date)}
        {#if i === runs.length - 1 || (i % labelEvery === 0 && x(runs.length - 1) - x(i) >= 56)}<text class="tick" x={x(i)} y={H - 8} text-anchor={i === 0 ? 'start' : i === runs.length - 1 ? 'end' : 'middle'}>{d.date.slice(5)}</text>{/if}
      {/each}
      {#if showCreated && showDone}<path class="gap" d={gap} use:chartGrow={{ delay: 500, duration: 700 }} />{/if}
      {#if showCreated}<path class="area created" d={areaC} use:chartGrow={{ duration: 700 }} />{/if}
      {#if showDone}<path class="area done" d={areaD} use:chartGrow={{ delay: 150, duration: 700 }} />{/if}
      {#if showCreated}<path class="line created" d={lineC} use:chartDraw />{/if}
      {#if showDone}<path class="line done" d={lineD} use:chartDraw={{ delay: 150 }} />{/if}
      {#if last}
        {#if showCreated}<circle class="dot created" cx={x(runs.length - 1)} cy={y(last.created)} r="4.5" /><text class="end created" x={x(runs.length - 1) + 9} y={y(last.created) + (showDone && Math.abs(y(last.created) - y(last.completed)) < 14 ? -4 : 4)}>{last.created}</text>{/if}
        {#if showDone}<circle class="dot done" cx={x(runs.length - 1)} cy={y(last.completed)} r="4.5" /><text class="end done" x={x(runs.length - 1) + 9} y={y(last.completed) + (showCreated && Math.abs(y(last.created) - y(last.completed)) < 14 ? 12 : 4)}>{last.completed}</text>{/if}
      {/if}
      {#if picked}
        <line class="cross" x1={x(selected)} x2={x(selected)} y1={T} y2={base} />
        {#if showCreated}<circle class="pick created" cx={x(selected)} cy={y(picked.created)} r="5" />{/if}
        {#if showDone}<circle class="pick done" cx={x(selected)} cy={y(picked.completed)} r="5" />{/if}
      {/if}
    </svg>
    {#if picked}<output class="readout" id={readoutId} style:left="{Math.min(Math.max(8, x(selected) - 80), width - 190)}px">{picked.date} · {picked.created} created · {picked.completed} done · {picked.created - picked.completed} difference</output>{/if}
  </div>
</div>

<style>
  .lp { min-width: 0; }
  .plot { position: relative; border-radius: 6px; }
  .plot:focus-visible { outline: 2px solid var(--accent); outline-offset: 4px; }
  svg { display: block; max-width: 100%; overflow: visible; }
  .grid { stroke: var(--border); stroke-dasharray: 2 4; }
  .tick { fill: var(--text-2); font: 12px var(--mono); }
  .area.created { fill: color-mix(in srgb, var(--accent) 13%, transparent); }
  .area.done { fill: color-mix(in srgb, var(--ok) 20%, transparent); }
  .gap { fill: color-mix(in srgb, var(--amber) 22%, transparent); }
  .line { fill: none; stroke-width: 2.6; stroke-linecap: round; stroke-linejoin: round; }
  .line.created, .dot.created, .pick.created { stroke: var(--accent); }
  .line.done, .dot.done, .pick.done { stroke: var(--ok); }
  .dot, .pick { stroke-width: 2.5; }
  .dot.created, .pick.created { fill: var(--surface); }
  .dot.done, .pick.done { fill: var(--surface); }
  .end { font: 600 12px var(--mono); }
  .end.created { fill: var(--accent); }
  .end.done { fill: var(--ok-text); }
  .cross { stroke: var(--text-2); stroke-dasharray: 3 3; }
  .legend { display: flex; flex-wrap: wrap; gap: 6px 16px; margin-bottom: 8px; color: var(--text-2); font-size: 12px; }
  .legend span { display: inline-flex; align-items: center; gap: 6px; }
  .k { width: 14px; height: 4px; border-radius: 2px; }
  .k.created { background: var(--accent); } .k.done { background: var(--ok); } .k.open { height: 10px; background: color-mix(in srgb, var(--amber) 55%, transparent); }
  .readout { position: absolute; top: 0; z-index: 2; padding: 7px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); box-shadow: var(--card-sh); font-size: 12px; pointer-events: none; max-width: calc(100% - 16px); white-space: normal; overflow-wrap: anywhere; }
</style>
