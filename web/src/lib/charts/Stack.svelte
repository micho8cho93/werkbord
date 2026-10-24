<script lang="ts">
  import { measuredNumber, percent } from '../analytics';
  import { chartGrow } from '../motion';
  export interface Part { label: string; value: number | null; tone: string; detail?: string; formatted?: string }
  // Parts of one whole laid end to end in a single bar. A part that was not reported is listed, with no length, rather than counted as zero.
  let { parts, label, total = null }: { parts: Part[]; label: string; total?: number | null } = $props();
  const sum = $derived(total ?? parts.reduce((n, p) => n + (p.value ?? 0), 0));
</script>

<div class="stack" role="group" aria-label={label}>
  <div class="bar" aria-hidden="true">
    {#each parts.filter((p) => (p.value ?? 0) > 0) as p, i (p.label)}
      <span class="seg" style:flex-grow={p.value} style:background={p.tone} use:chartGrow={{ from: 'left', delay: i * 120 }}></span>
    {/each}
  </div>
  <ul>
    {#each parts as p (p.label)}
      <li>
        <span class="sw" style:background={p.tone} aria-hidden="true"></span>
        <span class="nm">{p.label}{#if p.detail}<small>{p.detail}</small>{/if}</span>
        <strong class="mm">{p.formatted ?? measuredNumber(p.value)}</strong>
        <span class="pc">{p.value == null ? '' : (percent(p.value, sum) ?? 0) + '%'}</span>
      </li>
    {/each}
  </ul>
</div>

<style>
  .stack { display: grid; gap: 14px; }
  .bar { display: flex; gap: 3px; height: 22px; padding: 3px; border-radius: 8px; background: var(--surface-2); box-shadow: var(--tray-sh); }
  .seg { min-width: 4px; border-radius: 5px; }
  ul { display: grid; gap: 2px; margin: 0; padding: 0; list-style: none; }
  li { display: grid; grid-template-columns: 12px minmax(0, 1fr) auto 44px; align-items: baseline; gap: 8px; padding: 6px 8px; border-radius: 6px; font-size: 13px; }
  li:hover { background: var(--surface-2); }
  .sw { align-self: center; width: 12px; height: 12px; border-radius: 3px; }
  small { display: block; color: var(--text-2); font-size: 12px; line-height: 1.4; }
  .pc { color: var(--text-2); font-family: var(--mono); font-size: 12px; text-align: right; }
</style>
