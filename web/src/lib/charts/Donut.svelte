<script lang="ts">
  import { percent } from '../analytics';
  import { chartSweep } from '../motion';
  export interface Slice { label: string; value: number; tone: string }
  // A ring split into the parts of one whole, with the whole in its middle and every part named beside it. Pointing at a part, or at
  // its name, lifts it and quiets the rest.
  let { slices, centre, caption = '', label }: { slices: Slice[]; centre: string; caption?: string; label: string } = $props();
  const R = 52, C = 2 * Math.PI * R, GAP = 3;
  const total = $derived(slices.reduce((n, s) => n + s.value, 0));
  const arcs = $derived.by(() => {
    let at = 0;
    return slices.filter((s) => s.value > 0).map((s) => {
      const share = s.value / (total || 1);
      const length = Math.max(0, share * C - (slices.filter((x) => x.value > 0).length > 1 ? GAP : 0));
      const arc = { ...s, share, length, offset: -at };
      at += share * C;
      return arc;
    });
  });
  let active = $state('');
</script>

<div class="donut" role="group" aria-label={label}>
  <svg viewBox="0 0 140 140" aria-hidden="true">
    <g transform="rotate(-90 70 70)">
    <circle class="track" cx="70" cy="70" r={R} />
    {#each arcs as a, i (a.label)}
      <circle class="arc" class:dim={active && active !== a.label} class:lift={active === a.label} cx="70" cy="70" r={R} stroke={a.tone}
        stroke-dasharray="{a.length} {C - a.length}" stroke-dashoffset={a.offset}
        use:chartSweep={{ length: a.length, circumference: C, delay: i * 90 }}
        role="presentation" onpointerenter={() => (active = a.label)} onpointerleave={() => (active = '')} />
    {/each}
    </g>
    <text class="big" x="70" y="68" text-anchor="middle">{centre}</text>
    {#if caption}<text class="cap" x="70" y="86" text-anchor="middle">{caption}</text>{/if}
  </svg>
  <ul class="legend">
    {#each slices as s (s.label)}
      <li class:dim={active && active !== s.label} onpointerenter={() => (active = s.label)} onpointerleave={() => (active = '')}>
        <span class="sw" style:background={s.tone} aria-hidden="true"></span>
        <span class="nm">{s.label}</span>
        <strong class="mm">{s.value}</strong>
        <span class="pc">{percent(s.value, total) ?? 0}%</span>
      </li>
    {/each}
  </ul>
</div>

<style>
  .donut { display: grid; grid-template-columns: minmax(130px, 170px) minmax(0, 1fr); align-items: center; gap: 18px; }
  svg { display: block; width: 100%; height: auto; overflow: visible; }
  .track { fill: none; stroke: var(--surface-2); stroke-width: 16; }
  .arc { fill: none; stroke-width: 16; stroke-linecap: butt; transition: opacity 0.15s, stroke-width 0.15s; cursor: default; }
  .arc.dim { opacity: 0.3; }
  .arc.lift { stroke-width: 20; }
  .big { fill: var(--text); font: 600 22px var(--mono); }
  .cap { fill: var(--text-2); font: 12px var(--font); }
  .legend { display: grid; gap: 4px; margin: 0; padding: 0; list-style: none; }
  .legend li { display: grid; grid-template-columns: 12px minmax(0, 1fr) auto 44px; align-items: center; gap: 8px; padding: 5px 8px; border-radius: 6px; font-size: 13px; transition: opacity 0.15s, background 0.15s; }
  .legend li:hover { background: var(--surface-2); }
  .legend li.dim { opacity: 0.45; }
  .sw { width: 12px; height: 12px; border-radius: 3px; }
  .nm { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pc { color: var(--text-2); font-family: var(--mono); font-size: 12px; text-align: right; }
  @media (max-width: 520px) { .donut { grid-template-columns: 1fr; justify-items: center; } svg { max-width: 170px; } .legend { width: 100%; } }
</style>
