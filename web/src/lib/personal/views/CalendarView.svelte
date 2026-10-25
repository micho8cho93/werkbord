<script lang="ts">
  // A month of records placed on the day of a date field. Drag a record to another day to reschedule it.
  import Icon from '../../Icon.svelte';
  import RecordEditor from '../RecordEditor.svelte';
  import { workspaceSession } from '../session.svelte';
  import { matchesSavedFilters, type Collection, type PersonalRecord, type View } from '../schema';
  import { dayLabel, dayOf, isoDay, monthLabel, monthWeeks, onDay, weekdayLabels, weekStartsOn } from './calendar-grid';
  import { autofocus, newRecordValues, titleOf } from './shared';

  let { view, collection }: { view: View; collection: Collection } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const dateField = $derived(collection.fields.find((f) => f.key === view.dateField)!);
  const kind = $derived(dateField.type === 'datetime' ? 'datetime' : 'date');
  const canCreate = $derived((view.operations?.includes('create') ?? false) && !session.readOnly);
  const canUpdate = $derived((view.operations?.includes('update') ?? false) && !session.readOnly);
  const records = $derived((session.records[view.collectionId] ?? []).filter((r) => matchesSavedFilters(r.values, view.filters)));
  const weekStart = weekStartsOn();
  const today = new Date();
  let year = $state(today.getFullYear());
  let month = $state(today.getMonth());
  let selected = $state(isoDay(today));
  let adding = $state(false);
  let draftTitle = $state('');
  let editing = $state<PersonalRecord | null>(null);
  let dragging = $state('');
  let over = $state('');

  const weeks = $derived(monthWeeks(year, month, weekStart));
  const labels = $derived(weekdayLabels(weekStart));
  const byDay = $derived.by(() => {
    const found: Record<string, PersonalRecord[]> = {};
    for (const record of records) {
      const day = dayOf(record.values[view.dateField ?? ''], kind);
      if (day) (found[day] ??= []).push(record);
    }
    const map = { get: (day: string): PersonalRecord[] | undefined => found[day] };
    for (const list of Object.values(found)) list.sort((a, b) => String(a.values[view.dateField ?? '']).localeCompare(String(b.values[view.dateField ?? ''])) || titleOf(view, a).localeCompare(titleOf(view, b)));
    return map;
  });
  const undated = $derived(records.filter((r) => !dayOf(r.values[view.dateField ?? ''], kind)));
  const agenda = $derived(byDay.get(selected) ?? []);

  function shift(delta: number) {
    const next = new Date(year, month + delta, 1);
    year = next.getFullYear(); month = next.getMonth();
  }
  function goToday() { year = today.getFullYear(); month = today.getMonth(); selected = isoDay(today); }
  async function add(event: SubmitEvent) {
    event.preventDefault();
    const title = draftTitle.trim();
    if (!title) return;
    const given = { [view.fields[0]]: title, [view.dateField!]: onDay(undefined, kind, selected) };
    if (await session.mutate(view.id, 'create', undefined, newRecordValues(collection, view, given))) { draftTitle = ''; adding = false; }
  }
  async function reschedule(id: string, day: string) {
    const record = records.find((r) => r.id === id);
    if (!record || !canUpdate || dayOf(record.values[view.dateField!], kind) === day) return;
    await session.idle();
    await session.mutate(view.id, 'update', id, { [view.dateField!]: onDay(record.values[view.dateField!], kind, day) });
  }
  function drop(event: DragEvent, day: string) {
    event.preventDefault();
    const id = event.dataTransfer?.getData('text/plain') || dragging;
    dragging = ''; over = '';
    selected = day;
    if (id) void reschedule(id, day);
  }
  const timeOf = (record: PersonalRecord) => kind === 'datetime' && typeof record.values[view.dateField!] === 'string'
    ? new Date(record.values[view.dateField!] as string).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' }) : '';
</script>

<section class="cal" aria-label={view.name} data-view-kind="calendar">
  <div class="top">
    <h3>{view.name}</h3>
    <div class="nav">
      <button class="btn" type="button" aria-label="Previous month" onclick={() => shift(-1)}><Icon name="left" size={14} /></button>
      <strong class="month" aria-live="polite">{monthLabel(year, month)}</strong>
      <button class="btn" type="button" aria-label="Next month" onclick={() => shift(1)}><Icon name="right" size={14} /></button>
      <button class="btn" type="button" onclick={goToday}>Today</button>
    </div>
  </div>
  {#if editing}{#key editing.id}<RecordEditor {collection} {view} record={editing} onclose={() => editing = null} />{/key}{/if}
  <div class="grid" role="grid" aria-label={monthLabel(year, month)}>
    <div class="row head" role="row">{#each labels as label (label.long)}<div role="columnheader" aria-label={label.long}>{label.short}</div>{/each}</div>
    {#each weeks as week (week[0].date)}
      <div class="row" role="row">
        {#each week as day (day.date)}
          {@const list = byDay.get(day.date) ?? []}
          <div class="day" class:out={!day.inMonth} class:today={day.today} class:weekend={day.weekend} class:selected={selected === day.date} class:over={over === day.date} role="gridcell" tabindex="-1" aria-selected={selected === day.date}
            ondragover={(e) => { if (canUpdate && dragging) { e.preventDefault(); over = day.date; } }} ondragleave={() => { if (over === day.date) over = ''; }} ondrop={(e) => drop(e, day.date)}>
            <button type="button" class="num" aria-label={`${dayLabel(day.date)}, ${list.length} ${list.length === 1 ? 'entry' : 'entries'}`} onclick={() => { selected = day.date; adding = false; }}>{Number(day.date.slice(8))}</button>
            <ul>
              {#each list.slice(0, 3) as record (record.id)}
                <li><button type="button" class="chip" draggable={canUpdate} title={titleOf(view, record)} ondragstart={(e) => { dragging = record.id; e.dataTransfer?.setData('text/plain', record.id); }} ondragend={() => { dragging = ''; over = ''; }} onclick={() => { selected = day.date; }}>{titleOf(view, record)}</button></li>
              {/each}
              {#if list.length > 3}<li class="more">+{list.length - 3} more</li>{/if}
            </ul>
            {#if list.length}<span class="dot" aria-hidden="true">{list.length}</span>{/if}
          </div>
        {/each}
      </div>
    {/each}
  </div>

  <div class="agenda" aria-label={`Entries on ${dayLabel(selected)}`}>
    <div class="agenda-head"><h4>{dayLabel(selected)}</h4>
      {#if canCreate && !adding}<button class="btn add" type="button" disabled={session.busy} onclick={() => adding = true}><Icon name="plus" size={14} />Add</button>{/if}</div>
    {#if adding}
      <form class="adder" onsubmit={add}>
        <label class="visually-hidden" for={`${uid}-title`}>Title</label>
        <input id={`${uid}-title`} type="text" use:autofocus maxlength="300" placeholder={`New entry on ${dayLabel(selected)}`} bind:value={draftTitle} onkeydown={(e) => { if (e.key === 'Escape') { adding = false; draftTitle = ''; } }} />
        <button class="btn primary" type="submit" disabled={session.busy || !draftTitle.trim()}>Add</button><button class="btn" type="button" onclick={() => { adding = false; draftTitle = ''; }}>Cancel</button>
      </form>
    {/if}
    <ul>
      {#each agenda as record (record.id)}
        <li><span class="when">{timeOf(record)}</span><strong>{titleOf(view, record)}</strong>
          {#if canUpdate}<button class="btn" type="button" aria-label={`Edit ${titleOf(view, record)}`} disabled={session.busy || !!editing} onclick={() => editing = record}>Edit</button>{/if}
          {#if view.operations?.includes('delete') && !session.readOnly}<button class="btn danger" type="button" aria-label={`Delete ${titleOf(view, record)}`} disabled={session.busy} onclick={() => session.mutate(view.id, 'delete', record.id)}>Delete</button>{/if}</li>
      {:else}<li class="none">Nothing on this day.</li>{/each}
    </ul>
    {#if undated.length}<p class="undated">{undated.length} without a {dateField.name.toLocaleLowerCase()}: {undated.slice(0, 5).map((r) => titleOf(view, r)).join(', ')}{undated.length > 5 ? '…' : ''}</p>{/if}
  </div>
</section>

<style>
  .cal { display: grid; gap: 12px; min-width: 0; padding: 16px 18px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  :global(.card) .cal { padding: 0; border: 0; border-radius: 0; background: none; box-shadow: none; }
  .top { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; }
  h3 { margin: 0; font-size: 16px; font-weight: 600; }
  .nav { display: flex; align-items: center; gap: 6px; }
  .month { min-width: 9.5em; text-align: center; font-size: 14px; }
  .grid { border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
  .row { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); }
  .head div { padding: 6px 8px; background: var(--surface-2); color: var(--text-2); font-size: 12px; font-weight: 600; border-bottom: 1px solid var(--border); }
  .day { position: relative; min-width: 0; min-height: 92px; padding: 4px 5px 5px; border-right: 1px solid var(--border); border-bottom: 1px solid var(--border); background: var(--surface); }
  .row .day:last-child { border-right: 0; }
  .row:last-child .day { border-bottom: 0; }
  .day.weekend { background: color-mix(in srgb, var(--surface-2) 55%, var(--surface)); }
  .day.out { background: var(--surface-2); }
  .day.out .num { color: var(--text-2); opacity: 0.7; }
  .day.selected { box-shadow: inset 0 0 0 2px var(--accent); }
  .day.over { background: var(--tint); }
  .num { display: inline-grid; place-items: center; min-width: 24px; height: 24px; padding: 0 4px; border: 0; border-radius: 999px; background: transparent; color: var(--text); font: inherit; font-size: 13px; cursor: pointer; }
  .day.today .num { background: var(--accent); color: var(--accent-text); font-weight: 650; }
  .num:focus-visible, .chip:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  ul { margin: 0; padding: 0; list-style: none; }
  .day ul { display: grid; gap: 2px; margin-top: 3px; }
  .chip { display: block; width: 100%; padding: 1px 6px; border: 0; border-radius: 4px; background: var(--tint); color: var(--accent); font: inherit; font-size: 12px; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; cursor: pointer; }
  .chip[draggable='true'] { cursor: grab; }
  .more { padding-left: 6px; color: var(--text-2); font-size: 12px; }
  .dot { display: none; position: absolute; right: 5px; top: 5px; min-width: 18px; padding: 0 5px; border-radius: 999px; background: var(--tint); color: var(--accent); font-size: 11px; text-align: center; }
  .agenda { display: grid; gap: 8px; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); }
  .agenda-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  h4 { margin: 0; font-size: 14px; }
  .agenda li { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 6px 0; border-bottom: 1px solid var(--border); font-size: 13px; }
  .agenda li:last-child { border-bottom: 0; }
  .agenda li strong { flex: 1; min-width: 120px; font-weight: 550; overflow-wrap: anywhere; }
  .when { min-width: 4.5em; color: var(--text-2); font-size: 12px; }
  .none, .undated { color: var(--text-2); font-size: 13px; margin: 0; }
  .adder { display: flex; flex-wrap: wrap; gap: 6px; }
  .adder input { flex: 1 1 200px; min-width: 0; min-height: 34px; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 13px; }
  .adder input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  @media (max-width: 600px) {
    .day { min-height: 54px; padding: 2px; }
    .day ul { display: none; }
    .dot { display: block; top: auto; bottom: 3px; right: 3px; }
    .head div { padding: 6px 2px; text-align: center; font-size: 11px; }
    .num { min-width: 32px; height: 32px; } .adder input { min-height: 44px; font-size: 16px; } .agenda :global(.btn) { min-height: 44px; }
  }
</style>
