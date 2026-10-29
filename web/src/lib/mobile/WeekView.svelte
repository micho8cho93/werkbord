<script lang="ts">
  import { tick } from 'svelte';
  import { addDays } from '../calendar';
  import { formatTime, hourLabel } from '../clock.svelte';
  import { lanes } from '../calendar';
  import Icon from '../Icon.svelte';
  import { app } from '../state.svelte';
  import { localToday } from '../planning-dates';
  import CalRow from './CalRow.svelte';
  import { dayLayout, dayTitle, marks, weekDays, type CalItem } from './calendar';
  import { cal } from './calstate.svelte';

  // A week, one day at a time: the seven days across the top, and the chosen day's hours scrolling underneath, with each agent
  // start at its hour. Work planned for whole days sits above the hours, because it has no hour.
  let { items, onopen }: { items: CalItem[]; onopen: (item: CalItem) => void } = $props();

  const HOUR = 56;
  /** How long a start is drawn: a start has no end, so this is only enough to read and touch. */
  const SPAN = 50;
  const today = $derived(localToday(app.now));
  const days = $derived(weekDays(cal.anchor));
  const layout = $derived(dayLayout(items, cal.selected));
  const placed = $derived(lanes(layout.timed.map((item) => ({ start: item.minute ?? 0, item })), SPAN));
  const letters = ['M', 'T', 'W', 'T', 'F', 'S', 'S'];
  let scroller = $state<HTMLElement>();

  function pick(day: string) {
    cal.selected = day;
    cal.anchor = day;
  }
  function step(by: number) {
    pick(addDays(cal.selected, by * 7));
  }

  // Land on the first start of the day, or on the morning, or on now when the day is today.
  $effect(() => {
    void cal.selected;
    void layout.timed.length;
    const first = layout.timed.reduce((m, i) => Math.min(m, i.minute ?? 1440), 1440);
    const target = cal.selected === today ? new Date(app.now).getHours() * 60 - 60 : first < 1440 ? first - 30 : 7 * 60;
    void tick().then(() => scroller?.scrollTo({ top: Math.max(0, (target / 60) * HOUR), behavior: 'auto' }));
  });

  const nowLine = $derived(cal.selected === today ? ((new Date(app.now).getHours() * 60 + new Date(app.now).getMinutes()) / 60) * HOUR : -1);
</script>

<div class="week">
  <div class="nav">
    <button class="btn icon" type="button" onclick={() => step(-1)} aria-label="Previous week"><Icon name="left" size={16} /></button>
    <h2 aria-live="polite">{dayTitle(cal.selected)}</h2>
    <button class="btn small today" type="button" onclick={() => pick(today)}>Today</button>
    <button class="btn icon" type="button" onclick={() => step(1)} aria-label="Next week"><Icon name="right" size={16} /></button>
  </div>

  <div class="strip tray" role="group" aria-label="Days of the week">
    {#each days as day, i (day)}
      {@const m = marks(items, day)}
      <button type="button" class="d" class:today={day === today} aria-pressed={day === cal.selected} aria-label={dayTitle(day)} data-testid="week-day" onclick={() => pick(day)}>
        <span class="l mm">{letters[i]}</span>
        <span class="n">{Number(day.slice(8))}</span>
        <span class="dots" aria-hidden="true">
          {#if m.planned + m.milestone}<i class="plan"></i>{/if}
          {#if m.scheduled}<i class="sched"></i>{/if}
        </span>
      </button>
    {/each}
  </div>

  {#if layout.allDay.length}
    <section class="allday" aria-label="Planned for the day">
      <h3 class="lab">Planned</h3>
      <ul>
        {#each layout.allDay as item (item.key)}<li><CalRow {item} {onopen} /></li>{/each}
      </ul>
    </section>
  {/if}

  <!-- A scrolling region is reachable from the keyboard so that it can be scrolled with it. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="hours card" bind:this={scroller} data-testid="week-hours" tabindex="0" role="region" aria-label="Hours of {dayTitle(cal.selected)}">
    <div class="grid" style:height="{HOUR * 24}px">
      {#each Array.from({ length: 24 }, (_, h) => h) as h (h)}
        <div class="hour" style:top="{h * HOUR}px"><span class="mm">{hourLabel(h)}</span></div>
      {/each}
      {#if nowLine >= 0}<div class="now" style:top="{nowLine}px" aria-hidden="true"></div>{/if}
      {#each placed as p (p.item.key)}
        <button
          type="button"
          class="start"
          data-testid="week-start"
          style:top="{(p.start / 60) * HOUR}px"
          style:height="{(SPAN / 60) * HOUR - 3}px"
          style:left="calc(56px + (100% - 64px) * {p.lane / p.of})"
          style:width="calc((100% - 64px) / {p.of} - 4px)"
          onclick={() => onopen(p.item)}
        >
          <span class="t">{p.item.title}</span>
          <span class="mm">{p.item.at ? formatTime(p.item.at) : ''} · {p.item.projectName}</span>
        </button>
      {/each}
    </div>
    {#if layout.timed.length === 0}<p class="none">No agent starts on this day.</p>{/if}
  </div>
</div>

<style>
  .week {
    display: grid;
    gap: 12px;
  }

  .nav {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .nav h2 {
    flex: 1;
    margin: 0;
    font-size: 16px;
    font-weight: 600;
  }

  .nav .btn.icon {
    width: 44px;
    height: 44px;
    padding: 0;
  }

  .today {
    min-height: 44px;
  }

  .strip {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    gap: 2px;
    padding: 4px;
  }

  .d {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    min-height: 62px;
    padding: 6px 0;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text);
    font: inherit;
    -webkit-tap-highlight-color: transparent;
  }

  .d .n {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    font-size: 15px;
    font-weight: 500;
  }

  .d.today .n {
    background: var(--accent);
    color: var(--accent-text);
  }

  .d[aria-pressed='true'] {
    background: var(--surface);
    box-shadow: var(--btn-sh);
  }

  .dots {
    display: flex;
    gap: 3px;
    height: 6px;
  }

  i {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  i.plan {
    background: var(--accent);
  }

  i.sched {
    background: var(--amber);
  }

  .allday ul {
    list-style: none;
    margin: 8px 0 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .hours {
    position: relative;
    height: min(62dvh, 520px);
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .hours:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .grid {
    position: relative;
  }

  .hour {
    position: absolute;
    left: 0;
    right: 0;
    height: 0;
    border-top: 1px solid var(--border);
  }

  .hour span {
    position: absolute;
    top: 4px;
    left: 8px;
  }

  .now {
    position: absolute;
    left: 52px;
    right: 0;
    height: 2px;
    background: var(--accent);
    z-index: 2;
  }

  .start {
    position: absolute;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 10px;
    border: 1px solid color-mix(in srgb, var(--amber) 70%, var(--border));
    border-left-width: 4px;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--amber) 20%, var(--surface));
    color: var(--text);
    text-align: left;
    font: inherit;
    overflow: hidden;
    -webkit-tap-highlight-color: transparent;
  }

  .start .t {
    font-size: 13px;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .start:active {
    background: color-mix(in srgb, var(--amber) 32%, var(--surface));
  }

  .none {
    position: sticky;
    bottom: 0;
    margin: 0;
    padding: 10px 14px;
    color: var(--text-2);
    font-size: 13px;
    background: var(--surface);
  }
</style>
