<script lang="ts">
  import { addDays, visibleDays } from './calendar';
  import { localToday } from './timeline';
  export interface CalendarRow { id: string; title: string; project: string; start: string; end: string; href?: string; detail?: string; }
  let { rows, timeline = false, onopen }: { rows: CalendarRow[]; timeline?: boolean; onopen?: (id: string) => void } = $props();
  let hover = $state<{text: string; x: number; y: number} | null>(null);
  function showHover(event: MouseEvent | FocusEvent) {
    const el = (event.target as HTMLElement).closest<HTMLElement>('[data-tooltip]');
    if (!el) { hover = null; return; }
    const rect = el.getBoundingClientRect();
    hover = { text: el.dataset.tooltip ?? '', x: Math.max(12, Math.min(rect.left, innerWidth - 332)), y: Math.min(rect.bottom + 6, innerHeight - 80) };
  }
  let anchor = $state(localToday(Date.now()));
  const days = $derived(visibleDays(anchor, 'week'));
  const inWeek = $derived(rows.filter(r => r.start <= days[6] && r.end >= days[0]));
  const date = (d: string) => new Date(d + 'T12:00:00').toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' });
  const open = (r: CalendarRow) => { if (onopen) onopen(r.id); };
</script>
<section class="planning-calendar" onmouseover={showHover} onmouseout={() => hover = null} onfocusin={showHover} onfocusout={() => hover = null} aria-label={timeline ? 'Planning timeline' : 'Week calendar'}>
  <div class="tools"><button class="btn small" onclick={() => anchor = addDays(anchor, -7)} aria-label="Previous week">Previous</button><button class="btn small" onclick={() => anchor = localToday(Date.now())}>Today</button><button class="btn small" onclick={() => anchor = addDays(anchor, 7)} aria-label="Next week">Next</button><strong>{date(days[0])} – {date(days[6])}</strong></div>
  {#if !inWeek.length}<p class="empty">No scheduled or planned work this week.</p>{/if}
  <div class="scroll">
    {#if timeline}
      <div class="timeline-grid"><div class="axis"><span>Task</span>{#each days as d (d)}<time datetime={d}>{date(d)}</time>{/each}</div>
      {#each inWeek as r (r.id)}<div class="trow"><div class="name">{r.title}<small>{r.project}</small></div><div class="track"><button class="range" style:grid-column="{Math.max(0, days.indexOf(r.start)) + 1} / {r.end >= days[6] ? 8 : days.indexOf(r.end) + 2}" onclick={() => open(r)} aria-label="{r.title} · {r.start} – {r.end}" data-tooltip="{r.title} · {r.start} – {r.end}{r.detail ? ' · ' + r.detail : ''}">{r.title}</button></div></div>{/each}</div>
    {:else}
      <div class="week">{#each days as d (d)}<section class="day"><h2>{date(d)}</h2>{#each inWeek.filter(r => r.start <= d && r.end >= d) as r (r.id)}
        {#if onopen}<button class="event" onclick={() => open(r)}>{r.title}<small>{r.project}</small>{#if r.detail}<small>{r.detail}</small>{/if}</button>{:else}<a class="event" href={r.href}>{r.title}<small>{r.project}</small>{#if r.detail}<small>{r.detail}</small>{/if}</a>{/if}
      {/each}</section>{/each}</div>
    {/if}
  </div>
</section>
{#if hover}<div class="timeline-tooltip" role="tooltip" style:left="{hover.x}px" style:top="{hover.y}px">{hover.text}</div>{/if}
<style>
  .timeline-tooltip { position: fixed; z-index: 80; max-width: 320px; padding: 8px 12px; color: var(--text); background: var(--surface); box-shadow: var(--card-sh); border: 1px solid var(--border); border-radius: var(--radius-sm); overflow-wrap: anywhere; pointer-events: none; font-size: 12px; }

  .planning-calendar { padding: 16px 24px; height: 100%; overflow: auto; }
  .tools { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 16px; }
  .tools strong { font-size: 13px; margin-left: 8px; }
  .scroll { overflow: auto; }
  .week { display: grid; grid-template-columns: repeat(7, minmax(140px, 1fr)); min-height: 360px; }
  .day { border: 1px solid var(--border); border-right: 0; padding: 10px; }
  .day:last-child { border-right: 1px solid var(--border); }
  h2, time { font-size: 12px; font-weight: 500; color: var(--text-2); }
  h2 { margin: 0 0 16px; }
  .event { display: block; width: 100%; padding: 10px; margin: 0 0 8px; text-align: left; border: 0; border-radius: var(--radius-sm); background: var(--surface-2); color: var(--text); font-size: 13px; text-decoration: none; overflow-wrap: anywhere; }
  small { display: block; font-size: 11.5px; color: var(--text-2); margin-top: 4px; }
  .event:hover, .range:hover { background: var(--surface); box-shadow: var(--card-sh); }
  .timeline-grid { min-width: 900px; }
  .axis { display: grid; grid-template-columns: 260px repeat(7, minmax(90px, 1fr)); padding-block: 12px; border-bottom: 1px solid var(--border); }
  .trow { display: grid; grid-template-columns: 260px 1fr; border-bottom: 1px solid var(--border); min-height: 64px; }
  .name { padding: 10px 16px 10px 0; overflow-wrap: anywhere; font-size: 13px; }
  .track { display: grid; grid-template-columns: repeat(7, minmax(90px, 1fr)); align-items: center; }
  .range { border: 0; padding: 8px; border-radius: var(--radius-sm); background: var(--surface-2); color: var(--text); text-align: left; overflow-wrap: anywhere; font-size: 12px; }
  @media (max-width: 899px) { .planning-calendar { padding: 16px; height: auto; } .tools button { min-height: 44px; } }
</style>
