<script lang="ts">
  import { layout } from './motion';
  import { addDays, visibleDays } from './calendar';
  import { localToday } from './planning-dates';
  import { listenDates, type DateDrop } from './planningGestures';
  export interface CalendarRow { id: string; title: string; project: string; start: string; end: string; href?: string; detail?: string; status?: string; kind?: 'planned' | 'scheduled'; milestone?: boolean; }
  let { rows, dense = false, onopen, onmove }: { rows: CalendarRow[]; dense?: boolean; onopen?: (id: string) => void; onmove?: (drop: DateDrop) => Promise<void> } = $props();
  let surface = $state<HTMLElement>();
  let anchor = $state(localToday(Date.now()));
  let pending = $state<Record<string, boolean>>({});
  let error = $state('');
  const days = $derived(visibleDays(anchor, 'week'));
  const inWeek = $derived(rows.filter(r => r.start <= days[6] && r.end >= days[0]));
  const date = (d: string) => new Date(d + 'T12:00:00').toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' });
  const status = (r: CalendarRow) => (r.status || (r.kind === 'scheduled' ? 'scheduled' : 'planned')).replaceAll('_', ' ');
  const open = (r: CalendarRow) => { if (!pending[r.id]) onopen?.(r.id); };
  async function move(drop: DateDrop) {
    if (!onmove || pending[drop.id]) return;
    pending[drop.id] = true; error = '';
    try { await onmove(drop); }
    catch (e) { error = `This change could not be saved. ${e instanceof Error ? e.message : String(e)}`; }
    finally { pending[drop.id] = false; }
  }
  $effect(() => surface && listenDates(surface, drop => move(drop)));
</script>
<section class="planning-calendar" class:dense use:layout={{ revision: rows.map(r => `${r.id}:${r.start}:${r.end}`).join('|'), enabled: true, selector: '[data-plan-item]' }} bind:this={surface} data-plan-surface aria-label="Week calendar">
  <div class="tools">
    <button class="btn small" onclick={() => anchor = addDays(anchor, -7)} aria-label="Previous period">Previous</button>
    <button class="btn small" onclick={() => { anchor = localToday(Date.now()); }}>Today</button>
    <button class="btn small" onclick={() => anchor = addDays(anchor, 7)} aria-label="Next period">Next</button>
    <strong>{date(days[0])} – {date(days[6])}</strong>
  </div>
  <p class="guide">{onmove ? 'Drag to move dates. ' : ''}{onopen ? 'Click to edit here.' : ''}</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if !inWeek.length}<p class="empty">No scheduled or planned work this week.</p>{/if}
  <div class="scroll">
      <div class="week">{#each days as d (d)}<section class="day" class:today={d === localToday(Date.now())} data-plan-day={d}><h2>{date(d)}</h2>{#each inWeek.filter(r => r.start <= d && r.end >= d) as r (r.id)}
        <button class="event" data-status={r.status} data-kind={r.kind} data-plan-item={onmove ? r.id : undefined} data-plan-start={r.start} data-plan-end={r.end} aria-busy={pending[r.id] || false} onclick={() => open(r)}><span class="status">{status(r)}</span><strong>{r.title}</strong><small>{r.project}</small>{#if r.detail}<small>{r.detail}</small>{/if}</button>
      {/each}</section>{/each}</div>
  </div>
</section>
<style>
  .planning-calendar { padding: 16px 24px; height: 100%; min-height: 0; display: flex; flex-direction: column; }
  .dense { padding: 0; }
  .dense .week { grid-template-columns: repeat(7, minmax(104px, 1fr)); }
  .dense .day { padding: 8px 6px; }
  .dense .event { padding: 7px 8px; }
  .tools { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .tools strong { font-size: 13px; margin-left: 8px; }
  .guide { color: var(--text-2); font-size: 12px; margin: 10px 0 14px; }
  .scroll { overflow: auto; min-height: 0; flex: 1; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  .week { display: grid; grid-template-columns: repeat(7, minmax(150px, 1fr)); min-height: 100%; }
  .day { border-right: 1px solid var(--border); padding: 10px; background: var(--bg); }
  .day:last-child { border-right: 0; }
  .day.today h2 { color: var(--accent); }
  h2 { font-size: 12px; font-weight: 600; color: var(--text-2); margin: 0 0 16px; }
  .event { --tone: var(--text-2); border: 1px solid color-mix(in srgb, var(--tone) 65%, var(--border)); border-radius: var(--radius-sm); background: color-mix(in srgb, var(--tone) 18%, var(--surface)); color: var(--text); text-align: left; font: inherit; font-size: 13px; }
  [data-kind='planned'] { --tone: var(--accent); }
  [data-kind='scheduled'] { --tone: var(--warn); }
  [data-status='doing'], [data-status='in_progress'] { --tone: var(--accent); }
  [data-status='review'] { --tone: var(--warn); }
  [data-status='done'] { --tone: var(--ok); }
  .event { display: block; width: 100%; padding: 10px; margin: 0 0 10px; overflow-wrap: anywhere; box-shadow: 0 2px 4px #0000000c; }
  .event strong { display: block; font-weight: 500; }
  small { display: block; font-size: 12px; color: var(--text); opacity: .85; margin-top: 4px; }
  .status { display: block; font-size: 12px; text-transform: capitalize; margin-bottom: 5px; font-weight: 600; }
  .event:hover { border-color: var(--tone); background: color-mix(in srgb, var(--tone) 25%, var(--surface)); }
  /* Paper, Graphite and Cloud: an event is a hairline card with a thin edge in its tone, not a tinted block with a shadow. */
  :global(:root:not([data-style='classic'])) .event {
    border: 1px solid var(--border);
    box-shadow: inset 3px 0 0 var(--tone);
    background: var(--surface);
  }
  :global(:root:not([data-style='classic'])) .event { margin-bottom: 8px; }
  :global(:root:not([data-style='classic'])) .event:hover { border-color: var(--border); background: var(--surface-2); }
  :global(:root:not([data-style='classic'])) .day { background: var(--surface); }
  :global(:root:not([data-style='classic'])) .status { font-weight: 500; color: var(--text-2); }
  :global(:root:not([data-style='classic'])) small { opacity: 1; color: var(--text-2); }
  @media (max-width: 1000px) { .dense .week { grid-template-columns: repeat(7, minmax(150px, 1fr)); } }
  @media (max-width: 899px) { .planning-calendar { padding: 16px; min-height: 480px; } .tools button { min-height: 44px; } }
</style>
