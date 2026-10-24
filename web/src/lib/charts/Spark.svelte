<script lang="ts">
  import { smoothPath } from '../analytics';
  import { chartDraw, chartGrow } from '../motion';
  // A small line of one figure over time, for the tile it sits in. It carries no numbers of its own; the tile's figure is the number.
  let { values, tone = 'var(--accent)', label = '' }: { values: number[]; tone?: string; label?: string } = $props();
  const W = 120, H = 40, P = 3;
  const max = $derived(Math.max(1, ...values));
  const points = $derived((values.length > 1 ? values : [0, ...values, 0]).map((v, i, all) => [P + (i * (W - 2 * P)) / Math.max(1, all.length - 1), H - P - (v / max) * (H - 2 * P)] as [number, number]));
  const line = $derived(smoothPath(points));
  const area = $derived(points.length ? `${line} L${points[points.length - 1][0]},${H} L${points[0][0]},${H} Z` : '');
</script>

<svg class="spark" viewBox="0 0 {W} {H}" preserveAspectRatio="none" role="img" aria-label={label || undefined} aria-hidden={label ? undefined : 'true'} style:--tone={tone}>
  <path class="area" d={area} use:chartGrow={{ duration: 700 }} />
  <path class="line" d={line} vector-effect="non-scaling-stroke" use:chartDraw={{ duration: 800 }} />
</svg>

<style>
  .spark { display: block; width: 100%; height: 40px; overflow: visible; }
  .area { fill: color-mix(in srgb, var(--tone) 16%, transparent); }
  .line { fill: none; stroke: var(--tone); stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
</style>
