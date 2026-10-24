<script lang="ts">
  import { chartGrow } from '../motion';
  // Every day of the period as a square in a calendar of weeks, Monday first, darker where more happened. It shows where the busy
  // days were without any axis to read; each square names its day and its count.
  let { days, tone = 'var(--accent)', label }: { days: { date: string; value: number }[]; tone?: string; label: string } = $props();
  const max = $derived(Math.max(1, ...days.map((d) => d.value)));
  // The first row starts on the Monday on or before the first day (UTC, as the counts are).
  const first = $derived(days.length ? (new Date(days[0].date + 'T00:00:00Z').getUTCDay() + 6) % 7 : 0);
  const cells = $derived(days.map((d, i) => ({ ...d, col: (i + first) % 7, row: Math.floor((i + first) / 7), level: d.value === 0 ? 0 : Math.max(1, Math.ceil((d.value / max) * 4)) })));
  const rows = $derived(cells.length ? cells[cells.length - 1].row + 1 : 0);
  const mix = [0, 22, 42, 66, 90];
  const weekdays = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
</script>

<figure class="heat" aria-label={label} style:--tone={tone}>
  <div class="head" aria-hidden="true">{#each weekdays as w (w)}<span>{w}</span>{/each}</div>
  <div class="grid" style:grid-template-rows="repeat({rows}, 1fr)">
    {#each cells as c, i (c.date)}
      <span class="cell" data-level={c.level} style:grid-column={c.col + 1} style:grid-row={c.row + 1} style:--mix="{mix[c.level]}%" title="{c.date}: {c.value}" use:chartGrow={{ delay: Math.min(600, i * 8), duration: 320 }}><small>{c.date.slice(8)}</small></span>
    {/each}
  </div>
  <figcaption><span>Fewer</span>{#each mix as m (m)}<i style:--mix="{m}%"></i>{/each}<span>More</span></figcaption>
</figure>

<style>
  .heat { display: grid; gap: 6px; margin: 0; }
  .head, .grid { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: 4px; }
  .head span { color: var(--text-2); font: 11.5px var(--mono); text-align: center; }
  .cell { display: flex; align-items: flex-start; justify-content: flex-start; min-height: 24px; padding: 3px 5px; border-radius: 5px; background: color-mix(in srgb, var(--tone) var(--mix), var(--surface-2)); }
  .cell small { color: var(--text-2); font: 11.5px var(--mono); line-height: 1.2; }
  .cell[data-level='1'] small, .cell[data-level='2'] small, .cell[data-level='3'] small { color: var(--text); }
  .cell[data-level='3'] small { color: light-dark(var(--text), var(--accent-text)); }
  .cell[data-level='4'] small { color: var(--accent-text); }
  figcaption { display: flex; align-items: center; justify-content: flex-end; gap: 4px; margin-top: 4px; color: var(--text-2); font-size: 11.5px; }
  figcaption span { margin: 0 4px; }
  i { width: 12px; height: 12px; border-radius: 3px; background: color-mix(in srgb, var(--tone) var(--mix), var(--surface-2)); }
</style>
