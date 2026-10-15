<script lang="ts">
  import { tick } from 'svelte';
  import { app } from '../state.svelte';
  import {
    PX_PER_DAY,
    ROW_HEIGHT,
    arrows,
    barsFor,
    daysBetween,
    dependencyMap,
    firstRows,
    localToday,
    spanOf,
    ticks,
    viewRange,
    type Span,
    type Zoom,
  } from '../timeline';
  import { addDays } from '../calendar';
  import Segmented from './Segmented.svelte';
  import { localParts } from './calendar';
  import { cal } from './calstate.svelte';
  import type { Task } from '../types';

  // The planning timeline, for a phone: each task with dates is a bar across the days, a milestone a diamond, and what waits for what
  // an arrow, as on the desktop. It scrolls sideways and zooms to a week, a month or a quarter. A task's schedule (when an agent
  // starts) is a separate amber marker on its row, because the plan and the schedule are different things and one never moves the other.
  let { tasks, onopen }: { tasks: Task[]; onopen: (taskId: string, kind: 'planned' | 'scheduled') => void } = $props();

  const zooms = [
    { id: 'week', label: 'Week' },
    { id: 'month', label: 'Month' },
    { id: 'quarter', label: 'Quarter' },
  ] as const;

  const today = $derived(localToday(app.now));
  const px = $derived(PX_PER_DAY[cal.zoom as Zoom]);
  type Row = { task: Task; span: Span | null; start: string | null };
  const rows = $derived.by<Row[]>(() => {
    const out: Row[] = [];
    for (const t of tasks) {
      const span = spanOf(t);
      const o = t.orchestration;
      const start = o?.enabled && o.scheduledAt ? (localParts(o.scheduledAt)?.day ?? null) : null;
      if (span || start) out.push({ task: t, span, start });
    }
    return out.sort((a, b) => (a.span?.start ?? a.start ?? '').localeCompare(b.span?.start ?? b.start ?? ''));
  });
  const spans = $derived(rows.flatMap((r) => [...(r.span ? [r.span] : []), ...(r.start ? [{ start: r.start, end: r.start, milestone: false }] : [])]));
  const range = $derived(viewRange(spans, today));
  const width = $derived((daysBetween(range.start, range.end) + 1) * px);
  const axis = $derived(ticks(range, cal.zoom as Zoom));
  const todayX = $derived(daysBetween(range.start, today) * px + px / 2);
  const bars = $derived(barsFor(rows.map((r) => r.task), range, cal.zoom as Zoom));
  const rowOf = $derived(firstRows(rows.map((r) => r.task.id)));
  const lines = $derived(arrows(bars, rowOf, dependencyMap(rows.map((r) => r.task))));
  let scroller = $state<HTMLElement>();

  function centre(day: string) {
    if (!scroller) return;
    scroller.scrollTo({ left: Math.max(0, daysBetween(range.start, day) * px - scroller.clientWidth / 2), behavior: 'auto' });
  }
  /** Move by about a screen, so the dates do not jump. */
  function page(by: number) {
    scroller?.scrollBy({ left: by * scroller.clientWidth * 0.8, behavior: 'smooth' });
  }
  function zoom(z: Exclude<Zoom, 'day'>) {
    // Keep what is in the middle of the screen in the middle.
    const middle = scroller ? addDays(range.start, Math.round((scroller.scrollLeft + scroller.clientWidth / 2) / px)) : today;
    cal.zoom = z;
    void tick().then(() => centre(middle));
  }
  $effect(() => {
    void rows.length;
    void tick().then(() => centre(cal.anchor === today ? today : cal.anchor));
  });
</script>

<div class="timeline">
  <Segmented options={zooms} value={cal.zoom} onchange={zoom} label="Timeline zoom" />
  <div class="tools">
    <button class="btn small" type="button" onclick={() => page(-1)} aria-label="Earlier">Earlier</button>
    <button class="btn small" type="button" onclick={() => centre(today)}>Today</button>
    <button class="btn small" type="button" onclick={() => page(1)} aria-label="Later">Later</button>
  </div>

  {#if rows.length === 0}
    <p class="empty">No work has planned dates or a schedule yet. Dates are set on a task, on its Board or in its Edit sheet.</p>
  {:else}
    <!-- A scrolling region is reachable from the keyboard so that it can be scrolled with it. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div class="scroller card" bind:this={scroller} data-testid="timeline-scroll" tabindex="0" role="region" aria-label="Planning timeline, scrolls sideways">
      <div class="surface" style:width="{width}px" style:height="{rows.length * ROW_HEIGHT + 34}px">
        <div class="axis" aria-hidden="true">
          {#each axis as t (t.day)}<time class:major={t.major} style:left="{t.x}px" datetime={t.day}>{t.label}</time>{/each}
        </div>
        <div class="body" style:top="34px">
          {#each axis as t (t.day)}<span class="line" class:major={t.major} style:left="{t.x}px"></span>{/each}
          {#if todayX >= 0 && todayX <= width}<span class="today" style:left="{todayX}px" aria-hidden="true"></span>{/if}
          <svg class="arrows" {width} height={rows.length * ROW_HEIGHT} aria-hidden="true">
            {#each lines as a (a.from + a.to)}<path d={a.d} class:conflict={a.conflict} />{/each}
          </svg>
          {#each rows as r, i (r.task.id)}
            {@const b = bars.get(r.task.id)}
            <div class="row" style:top="{i * ROW_HEIGHT}px">
              {#if b}
                <button
                  type="button"
                  class="bar"
                  class:milestone={b.milestone}
                  data-status={r.task.state}
                  data-testid="timeline-bar"
                  style:left="{b.x + (b.milestone ? Math.max(0, (b.width - 22) / 2) : 0)}px"
                  style:width="{b.milestone ? 22 : Math.max(24, b.width - 2)}px"
                  aria-label="{r.task.title}, planned {b.span.start === b.span.end ? b.span.start : `${b.span.start} to ${b.span.end}`}"
                  onclick={() => onopen(r.task.id, 'planned')}
                ></button>
              {/if}
              {#if r.start}
                <button
                  type="button"
                  class="sched"
                  data-testid="timeline-start"
                  style:left="{daysBetween(range.start, r.start) * px + px / 2 - 11}px"
                  aria-label="{r.task.title}, an agent starts on {r.start}"
                  onclick={() => onopen(r.task.id, 'scheduled')}
                ></button>
              {/if}
              <span class="name" style:left="{(b ? b.x + (b.milestone ? 30 : 8) : daysBetween(range.start, r.start ?? today) * px + px / 2 + 16)}px">{r.task.title}</span>
            </div>
          {/each}
        </div>
      </div>
    </div>
    <p class="legend mm"><span class="k plan"></span>Planned dates <span class="k sched"></span>An agent starts <span class="k mile"></span>Milestone</p>
  {/if}
</div>

<style>
  .timeline {
    display: grid;
    gap: 12px;
  }

  .tools {
    display: flex;
    gap: 8px;
  }

  .tools .btn {
    flex: 1;
    min-height: 44px;
  }

  .empty {
    margin: 0;
    padding: 16px 4px;
    color: var(--text-2);
    font-size: 13px;
  }

  .scroller {
    max-height: min(60dvh, 520px);
    overflow: auto;
    overscroll-behavior: contain;
  }

  .scroller:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .surface {
    position: relative;
  }

  .axis {
    position: sticky;
    top: 0;
    z-index: 4;
    height: 34px;
    background: var(--surface-2);
    border-bottom: 1px solid var(--border);
  }

  .axis time {
    position: absolute;
    top: 0;
    height: 100%;
    padding: 9px 6px 0;
    border-left: 1px solid var(--border);
    white-space: nowrap;
    font-family: var(--mono);
    font-size: 11.5px;
    color: var(--text-2);
  }

  .axis time.major {
    color: var(--text);
    font-weight: 600;
  }

  .body {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    background: var(--bg);
  }

  .line {
    position: absolute;
    top: 0;
    bottom: 0;
    border-left: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  }

  .line.major {
    border-left-color: var(--border);
  }

  .today {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 2px;
    margin-left: -1px;
    background: var(--accent);
    opacity: 0.7;
    z-index: 1;
  }

  .arrows {
    position: absolute;
    left: 0;
    top: 0;
    pointer-events: none;
    z-index: 2;
  }

  .arrows path {
    fill: none;
    stroke: var(--text-2);
    stroke-width: 1.2;
  }

  .arrows path.conflict {
    stroke: var(--danger);
  }

  .row {
    position: absolute;
    left: 0;
    right: 0;
    height: 36px;
  }

  .bar,
  .sched {
    --tone: var(--accent);
    position: absolute;
    top: 6px;
    height: 24px;
    padding: 0;
    border: 1px solid color-mix(in srgb, var(--tone) 70%, var(--border));
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--tone) 22%, var(--surface));
    -webkit-tap-highlight-color: transparent;
    z-index: 3;
  }

  /* A finger is wider than a bar a day long: the touchable area reaches past it on each side without changing what is drawn. */
  .bar::before,
  .sched::before {
    content: '';
    position: absolute;
    inset: -6px -8px;
  }

  .bar[data-status='review'] {
    --tone: var(--ok);
  }

  .bar[data-status='done'] {
    --tone: var(--text-2);
  }

  .bar.milestone {
    border-radius: 3px;
    transform: rotate(45deg) scale(0.62);
    background: var(--tone);
  }

  .sched {
    --tone: var(--amber);
    width: 22px;
    border-radius: 50%;
    border-style: dashed;
  }

  .name {
    position: absolute;
    top: 8px;
    max-width: 70vw;
    padding: 0 4px;
    border-radius: 3px;
    background: color-mix(in srgb, var(--bg) 80%, transparent);
    font-size: 12.5px;
    font-weight: 500;
    line-height: 20px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    pointer-events: none;
    z-index: 4;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 10px;
    margin: 0;
  }

  .k {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
    margin-left: 6px;
  }

  .k.plan {
    background: var(--accent);
    margin-left: 0;
  }

  .k.sched {
    background: var(--amber);
    border-radius: 50%;
  }

  .k.mile {
    background: var(--text);
    transform: rotate(45deg) scale(0.8);
  }

  @media (prefers-reduced-motion: reduce) {
    .scroller {
      scroll-behavior: auto;
    }
  }
</style>
