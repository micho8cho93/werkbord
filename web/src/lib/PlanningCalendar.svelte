<script lang="ts">
  import { layout } from './motion';
  import { tick } from 'svelte';
  import { addDays, visibleDays } from './calendar';
  import { daysBetween, localToday, PX_PER_DAY, ticks, viewRange, type Zoom } from './timeline';
  import { listenDates, type DateDrop } from './planningGestures';
  export interface CalendarRow { id: string; title: string; project: string; start: string; end: string; href?: string; detail?: string; status?: string; kind?: 'planned' | 'scheduled'; milestone?: boolean; }
  let { rows, timeline = false, onopen, onmove }: { rows: CalendarRow[]; timeline?: boolean; onopen?: (id: string) => void; onmove?: (drop: DateDrop) => Promise<void> } = $props();
  let surface = $state<HTMLElement>();
  let scroll = $state<HTMLElement>();
  let anchor = $state(localToday(Date.now()));
  let zoom = $state<Zoom>('week');
  let pending = $state<Record<string, boolean>>({});
  let error = $state('');
  const days = $derived(visibleDays(anchor, 'week'));
  const inWeek = $derived(rows.filter(r => r.start <= days[6] && r.end >= days[0]));
  const range = $derived(viewRange(rows.map(r => ({ start: r.start, end: r.end, milestone: !!r.milestone })), anchor, 7, zoom === 'quarter' ? 100 : 35));
  const px = $derived(PX_PER_DAY[zoom]);
  const width = $derived((daysBetween(range.start, range.end) + 1) * px);
  const axis = $derived(ticks(range, zoom));
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
  function centre() { if (scroll && timeline) scroll.scrollLeft = Math.max(0, daysBetween(range.start, anchor) * px - Math.max(0, scroll.clientWidth - 260) / 2); }
  $effect(() => { void timeline; void anchor; if (scroll) requestAnimationFrame(centre); });
  async function scale(next: Zoom) {
    const day = scroll ? (scroll.scrollLeft + Math.max(0, scroll.clientWidth - 260) / 2) / px : daysBetween(range.start, anchor);
    zoom = next;
    await tick();
    if (scroll) scroll.scrollLeft = Math.max(0, day * px - Math.max(0, scroll.clientWidth - 260) / 2);
  }
  $effect(() => {
    if (!scroll) return;
    const el = scroll;
    const wheel = (e: WheelEvent) => {
      if (!timeline || !e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      const scales: Zoom[] = ['day', 'week', 'month', 'quarter'];
      const next = scales[Math.max(0, Math.min(3, scales.indexOf(zoom) + (e.deltaY > 0 ? 1 : -1)))];
      if (next !== zoom) void scale(next);
    };
    el.addEventListener('wheel', wheel, { passive: false });
    return () => el.removeEventListener('wheel', wheel);
  });
</script>
<section class="planning-calendar" use:layout={{ revision: rows.map(r => `${r.id}:${r.start}:${r.end}`).join('|'), enabled: true, selector: '[data-plan-item]' }} bind:this={surface} data-plan-surface aria-label={timeline ? 'Planning timeline' : 'Week calendar'}>
  <div class="tools">
    <button class="btn small" onclick={() => anchor = addDays(anchor, timeline ? -(zoom === 'quarter' ? 90 : zoom === 'month' ? 30 : zoom === 'day' ? 1 : 7) : -7)} aria-label="Previous period">Previous</button>
    <button class="btn small" onclick={() => { anchor = localToday(Date.now()); centre(); }}>Today</button>
    <button class="btn small" onclick={() => anchor = addDays(anchor, timeline ? (zoom === 'quarter' ? 90 : zoom === 'month' ? 30 : zoom === 'day' ? 1 : 7) : 7)} aria-label="Next period">Next</button>
    {#if timeline}<div class="seg" role="group" aria-label="Timeline zoom">{#each ['day', 'week', 'month', 'quarter'] as z (z)}<button aria-pressed={zoom === z} onclick={() => scale(z as Zoom)}>{z[0].toUpperCase() + z.slice(1)}</button>{/each}</div>{/if}
    <strong>{timeline ? date(anchor) : `${date(days[0])} – ${date(days[6])}`}</strong>
  </div>
  <p class="guide">{onmove ? 'Drag to move dates. ' : ''}{timeline && onmove ? 'Drag either edge to resize. Ctrl/⌘ + scroll to zoom. ' : ''}{onopen ? 'Click to edit here.' : ''}</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if !(timeline ? rows : inWeek).length}<p class="empty">No scheduled or planned work {timeline ? 'yet' : 'this week'}.</p>{/if}
  <div class="scroll" bind:this={scroll} data-plan-grid={timeline ? '' : undefined} data-plan-px={px}>
    {#if timeline}
      <div class="timeline-grid" style:width="{width + 260}px">
        <div class="axis"><span>Task / project</span><div class="dates" style:width="{width}px">{#each axis as t (t.day)}<time class:major={t.major} style:left="{t.x}px" datetime={t.day}>{zoom === 'day' || zoom === 'week' ? date(t.day) : t.label}</time>{/each}</div></div>
        {#each rows as r (r.id)}
          <div class="trow"><button class="name" onclick={() => open(r)}>{r.title}<small>{r.project}</small><span class="name-status">{status(r)}</span></button>
            <div class="track" style:width="{width}px">
              {#each axis as t (t.day)}<span class="line" class:major={t.major} style:left="{t.x}px"></span>{/each}
              <button class="range" data-status={r.status} data-kind={r.kind} data-plan-item={onmove ? r.id : undefined} data-plan-start={r.start} data-plan-end={r.end} aria-busy={pending[r.id] || false}
                style:left="{daysBetween(range.start, r.start) * px}px" style:width="{Math.max(24, (daysBetween(r.start, r.end) + 1) * px - 2)}px" onclick={() => open(r)} title="{r.title} · {r.project} · {r.start} – {r.end}" aria-label="{r.title}, {r.start} to {r.end}. Alt + arrows move; Alt + Shift + arrows resize.">
                {#if onmove && !r.milestone && r.kind !== 'scheduled'}<span data-plan-edge="start" aria-hidden="true"></span>{/if}
                {#if (daysBetween(r.start, r.end) + 1) * px >= 80}<span class="range-title">{r.title}</span><span class="status">{status(r)}</span>{/if}
                {#if onmove && !r.milestone && r.kind !== 'scheduled'}<span data-plan-edge="end" aria-hidden="true"></span>{/if}
              </button>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <div class="week">{#each days as d (d)}<section class="day" class:today={d === localToday(Date.now())} data-plan-day={d}><h2>{date(d)}</h2>{#each inWeek.filter(r => r.start <= d && r.end >= d) as r (r.id)}
        <button class="event" data-status={r.status} data-kind={r.kind} data-plan-item={onmove ? r.id : undefined} data-plan-start={r.start} data-plan-end={r.end} aria-busy={pending[r.id] || false} onclick={() => open(r)}><span class="status">{status(r)}</span><strong>{r.title}</strong><small>{r.project}</small>{#if r.detail}<small>{r.detail}</small>{/if}</button>
      {/each}</section>{/each}</div>
    {/if}
  </div>
</section>
<style>
  .planning-calendar { padding: 16px 24px; height: 100%; min-height: 0; display: flex; flex-direction: column; }
  .tools { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .tools strong { font-size: 13px; margin-left: 8px; }
  .guide { color: var(--text-2); font-size: 12px; margin: 10px 0 14px; }
  .scroll { overflow: auto; min-height: 0; flex: 1; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); }
  .week { display: grid; grid-template-columns: repeat(7, minmax(150px, 1fr)); min-height: 100%; }
  .day { border-right: 1px solid var(--border); padding: 10px; background: var(--bg); }
  .day:last-child { border-right: 0; }
  .day.today h2 { color: var(--accent); }
  h2 { font-size: 12px; font-weight: 600; color: var(--text-2); margin: 0 0 16px; }
  .event, .range { --tone: var(--text-2); border: 1px solid color-mix(in srgb, var(--tone) 65%, var(--border)); border-radius: var(--radius-sm); background: color-mix(in srgb, var(--tone) 18%, var(--surface)); color: var(--text); text-align: left; font: inherit; font-size: 13px; }
  [data-kind='planned'] { --tone: var(--accent); }
  [data-kind='scheduled'] { --tone: var(--warn); }
  [data-status='doing'], [data-status='in_progress'] { --tone: var(--accent); }
  [data-status='review'] { --tone: var(--warn); }
  [data-status='done'] { --tone: var(--ok); }
  .event { display: block; width: 100%; padding: 10px; margin: 0 0 10px; overflow-wrap: anywhere; box-shadow: 0 2px 4px #0000000c; }
  .event strong { display: block; font-weight: 500; }
  small { display: block; font-size: 11.5px; color: var(--text); opacity: .85; margin-top: 4px; }
  .status { display: block; font-size: 11.5px; text-transform: capitalize; margin-bottom: 5px; font-weight: 600; }
  .event:hover, .range:hover { border-color: var(--tone); background: color-mix(in srgb, var(--tone) 25%, var(--surface)); }
  .axis { display: flex; position: sticky; top: 0; z-index: 4; height: 42px; border-bottom: 1px solid var(--border); background: var(--surface-2); }
  .axis > span { position: sticky; left: 0; z-index: 5; width: 260px; flex: none; padding: 12px; border-right: 1px solid var(--border); background: var(--surface-2); font-size: 12px; font-weight: 600; }
  .dates { position: relative; }
  time { position: absolute; height: 100%; padding: 12px 6px; border-left: 1px solid var(--border); white-space: nowrap; font-size: 11.5px; color: var(--text-2); }
  time.major { color: var(--text); font-weight: 600; }
  .trow { display: flex; border-bottom: 1px solid var(--border); min-height: 64px; }
  .name { position: sticky; left: 0; z-index: 3; width: 260px; flex: none; border: 0; border-right: 1px solid var(--border); padding: 10px 12px; background: var(--surface); color: var(--text); text-align: left; overflow-wrap: anywhere; font-size: 13px; cursor: pointer; }
  .name-status { display: block; margin-top: 4px; text-transform: capitalize; font-size: 11.5px; color: var(--text-2); }
  .name:hover { background: var(--surface-2); }
  .track { position: relative; flex: none; background: var(--bg); }
  .line { position: absolute; top: 0; bottom: 0; border-left: 1px solid color-mix(in srgb, var(--border) 65%, transparent); }
  .line.major { border-left-color: var(--border); }
  .range { position: absolute; top: 8px; bottom: 8px; padding: 4px 12px; overflow: hidden; white-space: nowrap; }
  .range-title { display: block; overflow: hidden; text-overflow: ellipsis; }
  .range .status { margin: 3px 0 0; font-weight: 400; }
  @media (max-width: 899px) { .planning-calendar { padding: 16px; min-height: 480px; } .tools button { min-height: 44px; } }
</style>
