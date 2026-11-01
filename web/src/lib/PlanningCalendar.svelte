<script lang="ts">
  import { untrack } from 'svelte';
  import { layout } from './motion';
  import { addDays, weekStart } from './calendar';
  import { localToday } from './planning-dates';
  import { listenDates, type DateDrop } from './planningGestures';
  import { leaveWeek, weekLeftOn } from './calendar-memory';
  import Icon from './Icon.svelte';
  export interface CalendarRow { id: string; title: string; project: string; start: string; end: string; href?: string; detail?: string; status?: string; kind?: 'planned' | 'scheduled'; milestone?: boolean; /** HH:MM, when the item is planned or scheduled for a time of day. */ time?: string; }
  /**
   * `rows` are what is on the calendar; `undated` is work that has no date yet, which waits in a tray to be dragged onto a day. `dense` is a
   * calendar in a small panel: one week, and no tray. Each item is an element with data-plan-item, and each day a cell with data-plan-day,
   * which is how a drag finds where it was dropped (planning-drag.js).
   */
  let { rows, undated = [], dense = false, onopen, onmove, remember = '' }: { rows: CalendarRow[]; undated?: CalendarRow[]; dense?: boolean; onopen?: (id: string) => void; onmove?: (drop: DateDrop) => Promise<void>; remember?: string } = $props();

  type View = 'month' | 'week' | 'list';
  let surface = $state<HTMLElement>();
  let anchor = $state(untrack(() => weekLeftOn(remember)) ?? localToday(Date.now()));
  $effect(() => leaveWeek(remember, anchor));
  // A month is seven columns: too narrow to read or tap on a phone, where the list is what shows until the person picks a view.
  let chosen = $state<View | null>(null);
  let narrow = $state(false);
  $effect(() => {
    if (typeof matchMedia !== 'function') return;
    const query = matchMedia('(max-width: 700px)');
    narrow = query.matches;
    const change = () => (narrow = query.matches);
    query.addEventListener('change', change);
    return () => query.removeEventListener('change', change);
  });
  const view = $derived<View>(chosen ?? (dense ? 'week' : narrow ? 'list' : 'month'));
  let show = $state<'all' | 'planned' | 'scheduled'>('all');
  let project = $state('');
  let trayOpen = $state(true);
  let pending = $state<Record<string, boolean>>({});
  let open = $state<Record<string, boolean>>({});
  let error = $state('');
  const MAX = 3;
  const today = $derived(localToday(Date.now()));

  // ---- what is shown: filtered, then placed on days ----
  const projects = $derived([...new Set(rows.map(r => r.project))].sort());
  const filtered = $derived(rows.filter(r => (show === 'all' || r.kind === show) && (!project || r.project === project)));
  const noDate = $derived(undated.filter(r => !project || r.project === project));

  const monthOf = (d: string) => d.slice(0, 7);
  const firstOfMonth = (d: string) => d.slice(0, 8) + '01';
  function shiftMonth(d: string, by: number): string {
    const y = Number(d.slice(0, 4)), m = Number(d.slice(5, 7)) - 1 + by;
    const date = new Date(Date.UTC(y, m, 1, 12));
    return date.toISOString().slice(0, 8) + '01';
  }
  /** The days a view draws: whole weeks, so a month shows the days of the weeks it begins and ends in. */
  const days = $derived.by(() => {
    if (view === 'week') { const first = weekStart(anchor); return Array.from({ length: 7 }, (_, i) => addDays(first, i)); }
    if (view === 'list') return [];
    const first = weekStart(firstOfMonth(anchor));
    const last = addDays(firstOfMonth(shiftMonth(anchor, 1)), -1);
    const count = Math.ceil((Date.parse(last + 'T12:00Z') - Date.parse(first + 'T12:00Z')) / 86_400_000 / 7 + 1 / 7) * 7;
    return Array.from({ length: count }, (_, i) => addDays(first, i));
  });
  const weeks = $derived(Array.from({ length: Math.ceil(days.length / 7) }, (_, i) => days.slice(i * 7, i * 7 + 7)));
  const long = (d: string, o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat([], { ...o, timeZone: 'UTC' }).format(new Date(d + 'T12:00:00Z'));
  const title = $derived(view === 'month' ? long(anchor, { month: 'long', year: 'numeric' }) : view === 'week' ? `${long(days[0], { month: 'short', day: 'numeric' })} – ${long(days[6], { month: 'short', day: 'numeric', year: 'numeric' })}` : 'Upcoming');
  const dow = (d: string) => long(d, { weekday: 'short' });

  const onDay = (r: CalendarRow, d: string) => r.start <= d && r.end >= d;
  /** A day's items: those with no time first (they are for the day, not an hour of it), then by time. */
  const itemsOn = (d: string) => filtered.filter(r => onDay(r, d)).sort((a, b) => (a.time ? 1 : 0) - (b.time ? 1 : 0) || (a.time ?? '').localeCompare(b.time ?? '') || a.title.localeCompare(b.title));
  const upcoming = $derived(filtered.filter(r => r.end >= today).sort((a, b) => a.start.localeCompare(b.start) || (a.time ?? '').localeCompare(b.time ?? '')).slice(0, 120));
  const inRange = $derived(view === 'list' ? upcoming.length : filtered.filter(r => r.start <= days[days.length - 1] && r.end >= days[0]).length);

  const COLOURS = ['var(--accent)', '#0a7c86', '#a0457e', '#5f7f2a', '#7b4fc0', '#b7791f'];
  /** One colour per project, the same every time, so a glance says which project an item is in. */
  function colour(project: string): string {
    let h = 0;
    for (const c of project) h = (h * 31 + c.charCodeAt(0)) >>> 0;
    return COLOURS[h % COLOURS.length];
  }
  const clock = (t: string) => new Date(`2000-01-01T${t}:00`).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const status = (r: CalendarRow) => (r.status || (r.kind === 'scheduled' ? 'scheduled' : 'planned')).replaceAll('_', ' ');

  function step(by: number) {
    anchor = view === 'month' ? shiftMonth(anchor, by) : addDays(anchor, 7 * by);
  }
  const openItem = (r: CalendarRow) => { if (!pending[r.id]) onopen?.(r.id); };
  async function move(drop: DateDrop) {
    if (!onmove || pending[drop.id]) return;
    pending[drop.id] = true; error = '';
    try { await onmove(drop); }
    catch (e) { error = `This change could not be saved. ${e instanceof Error ? e.message : String(e)}`; }
    finally { pending[drop.id] = false; }
  }
  $effect(() => surface && listenDates(surface, drop => move(drop)));
</script>

{#snippet chip(r: CalendarRow, d: string, full: boolean)}
  <button
    class="event cc" class:full class:continues={r.start < d} class:goes-on={r.end > d} class:done={r.status === 'done'}
    style:--bar={colour(r.project)}
    data-status={r.status} data-kind={r.kind} data-plan-item={onmove ? r.id : undefined} data-plan-start={r.start} data-plan-end={r.end}
    aria-busy={pending[r.id] || false} title="{r.title} · {r.project}" onclick={() => openItem(r)}
  >
    <span class="line">
      {#if r.time}<span class="at">{clock(r.time)}</span>{/if}
      {#if r.kind === 'scheduled'}<span class="run" role="img" aria-label="Agent run"><Icon name="play" size={10} /></span>{/if}
      <strong>{r.title}</strong>
    </span>
    {#if full}<small>{r.project} · {status(r)}</small>{/if}
  </button>
{/snippet}

<section class="planning-calendar" class:dense use:layout={{ revision: rows.map(r => `${r.id}:${r.start}:${r.end}:${r.time ?? ''}`).join('|'), enabled: true, selector: '[data-plan-item]' }} bind:this={surface} data-plan-surface aria-label="Calendar">
  <div class="tools">
    <div class="nav">
      <button class="btn small" onclick={() => { anchor = today; }}>Today</button>
      <button class="btn small icon" onclick={() => step(-1)} aria-label="Previous period" disabled={view === 'list'}><Icon name="left" size={14} /></button>
      <button class="btn small icon" onclick={() => step(1)} aria-label="Next period" disabled={view === 'list'}><Icon name="right" size={14} /></button>
      <h2 class="period" aria-live="polite">{title}</h2>
    </div>
    <div class="filters">
      {#if !dense}
        <label class="visually-hidden" for="cal-show">Show</label>
        <select id="cal-show" class="select" bind:value={show}><option value="all">All items</option><option value="planned">Planned work</option><option value="scheduled">Agent runs</option></select>
        {#if projects.length > 1}<label class="visually-hidden" for="cal-project">Project</label>
        <select id="cal-project" class="select" bind:value={project}><option value="">All projects</option>{#each projects as p (p)}<option value={p}>{p}</option>{/each}</select>{/if}
        {#if onmove}<button class="btn small" class:on={trayOpen} aria-pressed={trayOpen} onclick={() => (trayOpen = !trayOpen)}>No date{#if noDate.length}<span class="count">{noDate.length}</span>{/if}</button>{/if}
      {/if}
      <div class="seg" role="group" aria-label="View">
        <button aria-pressed={view === 'month'} onclick={() => (chosen = 'month')} hidden={dense}>Month</button>
        <button aria-pressed={view === 'week'} onclick={() => (chosen = 'week')}>Week</button>
        <button aria-pressed={view === 'list'} onclick={() => (chosen = 'list')} hidden={dense}>List</button>
      </div>
    </div>
  </div>
  <p class="guide">{onmove && view !== 'list' ? 'Drag to move dates. ' : ''}{onopen ? 'Click to edit here.' : ''}</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if !inRange}<p class="empty">{view === 'list' ? 'Nothing is planned or scheduled from today.' : `No scheduled or planned work this ${view}.`}</p>{/if}

  <div class="stage" class:with-tray={onmove && trayOpen && !dense && view !== 'list'}>
    <div class="scroll">
      {#if view === 'list'}
        <ol class="list">
          {#each upcoming as r (r.id)}
            <li><span class="when">{long(r.start, { weekday: 'short', month: 'short', day: 'numeric' })}{r.end !== r.start ? ` – ${long(r.end, { month: 'short', day: 'numeric' })}` : ''}</span>{@render chip(r, r.start, true)}</li>
          {/each}
        </ol>
      {:else}
        {#if view === 'month'}<div class="dows" aria-hidden="true">{#each weeks[0] ?? [] as d (d)}<span>{dow(d)}</span>{/each}</div>{/if}
        <div class="grid" class:month={view === 'month'}>
          {#each days as d (d)}
            {@const items = itemsOn(d)}
            {@const all = open[d] || view === 'week'}
            <section class="day" class:today={d === today} class:outside={view === 'month' && monthOf(d) !== monthOf(anchor)} data-plan-day={d} aria-label={long(d, { weekday: 'long', month: 'long', day: 'numeric' })}>
              <h3><span class="n">{view === 'month' ? Number(d.slice(8)) : `${dow(d)} ${Number(d.slice(8))}`}</span>{#if view === 'month' && Number(d.slice(8)) === 1}<span class="m">{long(d, { month: 'short' })}</span>{/if}</h3>
              {#each (all ? items : items.slice(0, MAX)) as r (r.id + d)}{@render chip(r, d, view === 'week')}{/each}
              {#if view === 'month' && items.length > MAX}
                <button class="more" onclick={() => (open[d] = !open[d])} aria-expanded={!!open[d]}>{open[d] ? 'Show less' : `${items.length - MAX} more`}</button>
              {/if}
            </section>
          {/each}
        </div>
      {/if}
    </div>
    {#if onmove && trayOpen && !dense && view !== 'list'}
      <aside class="tray nodate" aria-label="No date">
        <header><h3 class="section-title">No date</h3><span class="tray-note">{noDate.length}</span></header>
        {#if noDate.length}
          <ul class="tray-card tray-list">{#each noDate as r (r.id)}<li>
            <button class="event cc full" style:--bar={colour(r.project)} data-kind="planned" data-plan-item={r.id} data-plan-start={today} data-plan-end={today} aria-busy={pending[r.id] || false} title="{r.title} · {r.project}" onclick={() => openItem(r)}><span class="line"><strong>{r.title}</strong></span><small>{r.project}</small></button>
          </li>{/each}</ul>
          <p class="hint">Drag onto a day to plan it. Planning never starts an agent.</p>
        {:else}<p class="hint">Everything has a date.</p>{/if}
      </aside>
    {/if}
  </div>
</section>

<style>
  .planning-calendar { padding: 16px 24px; height: 100%; min-height: 0; display: flex; flex-direction: column; gap: 0; }
  .dense { padding: 0; }
  .tools { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 16px; }
  .nav, .filters { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .period { margin: 0 0 0 8px; font-family: var(--font); font-size: var(--fs-section); font-weight: 600; letter-spacing: -.012em; text-transform: none; color: var(--text); }
  .filters .select { width: auto; max-width: 180px; }
  .filters .count { margin-left: 6px; padding: 0 6px; border-radius: 999px; background: var(--surface-2); font-size: 12px; font-variant-numeric: tabular-nums; }
  .filters .btn.on { background: var(--selected); }
  .guide { color: var(--text-2); font-size: 12px; margin: 10px 0 12px; }
  .empty { margin: 0 0 12px; color: var(--text-2); font-size: 13px; }
  .stage { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; }
  .stage.with-tray { grid-template-columns: minmax(0, 1fr) minmax(13rem, 17rem); }
  .scroll { overflow: auto; min-height: 0; border: 1px solid var(--edge-strong); border-radius: var(--radius); background: var(--surface); }

  .dows { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); border-bottom: 1px solid var(--border); background: var(--tray); position: sticky; top: 0; z-index: 1; }
  .dows span { padding: 8px 10px; font-size: 12px; font-weight: 600; color: var(--text-2); }
  .grid { display: grid; grid-template-columns: repeat(7, minmax(150px, 1fr)); min-height: 100%; }
  .grid.month { grid-template-columns: repeat(7, minmax(0, 1fr)); grid-auto-rows: minmax(7.5rem, 1fr); }
  .day { border-right: 1px solid var(--border); border-bottom: 1px solid var(--border); padding: 6px; display: flex; flex-direction: column; gap: 4px; min-width: 0; background: var(--surface); }
  .day:nth-child(7n) { border-right: 0; }
  .day.outside { background: var(--tray); }
  .day.outside h3 .n { color: var(--text-2); }
  .day h3 { margin: 0 0 2px; display: flex; align-items: baseline; gap: 6px; font-size: 12px; font-weight: 600; font-family: var(--font); text-transform: none; letter-spacing: 0; color: var(--text); }
  .day .m { color: var(--text-2); font-weight: 500; }
  .day.today h3 .n { display: inline-grid; place-items: center; min-width: 22px; height: 22px; padding: 0 4px; border-radius: 999px; background: var(--accent); color: var(--accent-text); }

  /* An item: a chip with a bar in its project's colour, the time if it has one, then its title. */
  .cc { --bar: var(--accent); display: block; width: 100%; min-width: 0; padding: 3px 8px 3px 10px; border: 1px solid var(--edge-strong); border-radius: 6px; background: var(--surface); box-shadow: inset 3px 0 0 var(--bar); color: var(--text); font: inherit; font-size: 12px; text-align: left; }
  .cc:hover { background: var(--hover); }
  .cc .line { display: flex; align-items: center; gap: 6px; min-width: 0; min-height: 20px; }
  .cc strong { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .cc .at { flex: none; font-variant-numeric: tabular-nums; color: var(--text-2); }
  .cc .run { flex: none; display: inline-flex; color: var(--text-2); }
  .cc.done { opacity: .6; }
  .cc.done strong { text-decoration: line-through; }
  .cc.continues { border-top-left-radius: 0; border-bottom-left-radius: 0; }
  .cc.goes-on { border-top-right-radius: 0; border-bottom-right-radius: 0; }
  .cc.full { padding: 7px 10px 7px 12px; }
  .cc.full strong { white-space: normal; }
  .cc small { display: block; margin-top: 2px; color: var(--text-2); font-size: 12px; text-transform: capitalize; }
  .cc[data-kind='scheduled'] { background: color-mix(in srgb, var(--warn) 7%, var(--surface)); }
  .more { align-self: flex-start; border: 0; background: none; padding: 0 4px; color: var(--text-2); font: inherit; font-size: 12px; cursor: pointer; }
  .more:hover { color: var(--text); text-decoration: underline; }

  .list { list-style: none; margin: 0; padding: 8px 12px; display: grid; gap: 6px; }
  .list li { display: grid; grid-template-columns: 9rem minmax(0, 1fr); gap: 12px; align-items: center; }
  .list .when { font-size: 13px; color: var(--text-2); }

  .nodate { align-self: start; position: sticky; top: 0; }
  .tray-list { list-style: none; margin: 0; padding: 8px; display: grid; gap: 6px; }
  .hint { margin: 0; font-size: 12px; color: var(--text-2); line-height: 1.5; }
  @media (max-width: 1000px) { .stage.with-tray { grid-template-columns: minmax(0, 1fr); } .nodate { position: static; } }
  @media (max-width: 899px) { .planning-calendar { padding: 16px; min-height: 480px; } .tools button { min-height: 44px; } .grid.month { grid-template-columns: repeat(7, minmax(88px, 1fr)); } }
</style>
