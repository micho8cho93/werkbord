<script lang="ts">
  import Icon from '../Icon.svelte';
  import { localToday } from '../timeline';
  import { app } from '../state.svelte';
  import CalRow from './CalRow.svelte';
  import { dayTitle, itemsOn, marks, monthGrid, monthStart, monthTitle, shiftMonth, type CalItem } from './calendar';
  import { cal } from './calstate.svelte';

  // A familiar month. Each day carries small marks for what it holds (a cobalt dot for planned work, an amber one for an agent's
  // start, a diamond for a milestone); touching a day lists its items underneath.
  let { items, onopen }: { items: CalItem[]; onopen: (item: CalItem) => void } = $props();

  const today = $derived(localToday(app.now));
  const grid = $derived(monthGrid(cal.anchor));
  const month = $derived(cal.anchor.slice(0, 7));
  const here = $derived(itemsOn(items, cal.selected));
  const names = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

  function go(by: number) {
    cal.anchor = shiftMonth(cal.anchor, by);
    // The list under the grid follows: today when the month is this one, else its first day.
    cal.selected = cal.anchor.slice(0, 7) === today.slice(0, 7) ? today : monthStart(cal.anchor);
  }
  function now() {
    cal.anchor = today;
    cal.selected = today;
  }
</script>

<div class="month">
  <div class="nav">
    <button class="btn icon" type="button" onclick={() => go(-1)} aria-label="Previous month"><Icon name="left" size={16} /></button>
    <h2 aria-live="polite">{monthTitle(cal.anchor)}</h2>
    <button class="btn small today" type="button" onclick={now}>Today</button>
    <button class="btn icon" type="button" onclick={() => go(1)} aria-label="Next month"><Icon name="right" size={16} /></button>
  </div>

  <div class="grid tray" role="grid" aria-label={monthTitle(cal.anchor)}>
    <div class="names" role="row">
      {#each names as n (n)}<span role="columnheader">{n}</span>{/each}
    </div>
    {#each grid as week (week[0])}
      <div class="week" role="row">
        {#each week as day (day)}
          {@const m = marks(items, day)}
          <button
            class="day"
            type="button"
            role="gridcell"
            data-testid="cal-day"
            data-day={day}
            class:out={day.slice(0, 7) !== month}
            class:today={day === today}
            aria-selected={day === cal.selected}
            aria-label="{dayTitle(day)}{m.planned + m.scheduled + m.milestone ? `, ${m.planned + m.scheduled + m.milestone} item${m.planned + m.scheduled + m.milestone === 1 ? '' : 's'}` : ''}"
            onclick={() => (cal.selected = day)}
          >
            <span class="n">{Number(day.slice(8))}</span>
            <span class="marks" aria-hidden="true">
              {#if m.planned}<i class="plan"></i>{/if}
              {#if m.scheduled}<i class="sched"></i>{/if}
              {#if m.milestone}<i class="mile"></i>{/if}
            </span>
          </button>
        {/each}
      </div>
    {/each}
  </div>

  <div class="legend mm" aria-hidden="true">
    <span><i class="plan"></i>Planned</span><span><i class="sched"></i>Agent starts</span><span><i class="mile"></i>Milestone</span>
  </div>

  <section class="list" aria-label={dayTitle(cal.selected)}>
    <h3 class="lab">{dayTitle(cal.selected)}</h3>
    {#if here.length === 0}
      <p class="none">Nothing planned or scheduled.</p>
    {:else}
      <ul>
        {#each here as item (item.key)}<li><CalRow {item} {onopen} /></li>{/each}
      </ul>
    {/if}
  </section>
</div>

<style>
  .month {
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

  .grid {
    padding: 8px 6px 6px;
    display: grid;
    gap: 2px;
  }

  .names,
  .week {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    gap: 2px;
  }

  .names span {
    padding: 2px 0 6px;
    text-align: center;
    font-family: var(--mono);
    font-size: 11.5px;
    color: var(--text-2);
  }

  .day {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-start;
    gap: 4px;
    min-height: 48px;
    padding: 6px 0 4px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text);
    font: inherit;
    -webkit-tap-highlight-color: transparent;
  }

  /* Days of the neighbouring months: the secondary ink, not a faded one, so they stay legible. */
  .day.out {
    color: var(--text-2);
  }

  .n {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    font-size: 14px;
    font-weight: 500;
  }

  .day.today .n {
    background: var(--accent);
    color: var(--accent-text);
  }

  /* The day shown below is the pressed-in one, as the current item of the design system is. */
  .day[aria-selected='true'] {
    background: var(--surface);
    box-shadow: var(--btn-sh);
  }

  .marks {
    display: flex;
    gap: 3px;
    height: 6px;
  }

  i {
    display: inline-block;
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

  i.mile {
    border-radius: 1px;
    background: var(--text);
    transform: rotate(45deg) scale(0.85);
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
  }

  .legend span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .list h3 {
    margin: 4px 0 8px;
  }

  .list ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .none {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }
</style>
