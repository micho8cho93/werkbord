<script lang="ts">
  import { chartSweep } from '../motion';
  // How far one thing has got, as a ring. A value of null is "not known", drawn empty with a dash, never as 0%.
  let { value, tone = 'var(--ok)', caption = '', label }: { value: number | null; tone?: string; caption?: string; label: string } = $props();
  const R = 50, C = 2 * Math.PI * R;
  const length = $derived(value == null ? 0 : Math.min(1, Math.max(0, value)) * C);
</script>

<figure class="ring" aria-label={label}>
  <svg viewBox="0 0 140 140" aria-hidden="true">
    <circle class="track" cx="70" cy="70" r={R} />
    {#if length > 0}<circle class="arc" cx="70" cy="70" r={R} stroke={tone} stroke-dasharray="{length} {C - length}" use:chartSweep={{ length, circumference: C }} />{/if}
  </svg>
  <div class="mid"><strong class="mm">{value == null ? '–' : Math.round(value * 100) + '%'}</strong>{#if caption}<span>{caption}</span>{/if}</div>
</figure>

<style>
  .ring { position: relative; display: grid; place-items: center; width: min(100%, 170px); margin: 0 auto; }
  svg { display: block; width: 100%; height: auto; transform: rotate(-90deg); }
  .track { fill: none; stroke: var(--surface-2); stroke-width: 14; }
  .arc { fill: none; stroke-width: 14; stroke-linecap: round; }
  .mid { position: absolute; display: grid; justify-items: center; gap: 2px; text-align: center; }
  strong { font-size: 26px; font-weight: 600; }
  span { max-width: 90px; color: var(--text-2); font-size: 11.5px; line-height: 1.3; }
</style>
