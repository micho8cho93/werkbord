<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../lib/api';
  import { addDays, clockAt, lanes, minuteOf, rangeTitle, visibleDays } from '../lib/calendar';
  import { agentLabel, resolveFor } from '../lib/execution';
  import { runStatus } from '../lib/format';
  import Icon from '../lib/Icon.svelte';
  import { taskHref } from '../lib/router.svelte';
  import { editableOrchestration, emptyOrchestration, instantFromWall, schedulingLabels, wallTime } from '../lib/scheduling';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import type { Project, Task } from '../lib/types';

  // The same tasks as the Board, placed in time. Drag a task onto the week (or pick it and click a
  // time); the controller starts it then, browser open or not. Dependencies, order, deadlines and
  // missed-time policy are set on the task.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  const HOUR = 48; // px per hour
  const EVENT_MIN = 45; // how long an event looks: a schedule is a start time, not a duration
  const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

  let zone = $state(localZone);
  let zoneDraft = $state(localZone);
  let editingZone = $state(false);
  const zoneValid = (z: string) => {
    try {
      new Intl.DateTimeFormat(undefined, { timeZone: z });
      return true;
    } catch {
      return false;
    }
  };
  const today = $derived(wallTime(new Date(app.now).toISOString(), zone).slice(0, 10));
  const nowMinute = $derived(minuteOf(wallTime(new Date(app.now).toISOString(), zone)));

  let view = $state<'day' | 'week'>(typeof window !== 'undefined' && window.innerWidth < 900 ? 'day' : 'week');
  let anchor = $state(untrack(() => wallTime(new Date().toISOString(), localZone).slice(0, 10)));
  const days = $derived(visibleDays(anchor, view));
  const step = $derived(view === 'day' ? 1 : 7);

  // ---- what is where ----
  const active = (t: Task) => {
    const r = scope.latestRun[t.id];
    return !!r && runStatus(r).active;
  };
  const scheduledIn = (t: Task) => (t.orchestration?.scheduledAt ? wallTime(t.orchestration.scheduledAt, zone) : '');
  const unscheduled = $derived(scope.activeTasks.filter((t) => t.state !== 'done' && !active(t) && !(t.orchestration?.enabled && t.orchestration.scheduledAt)));
  const upcoming = $derived(
    scope.activeTasks
      .filter((t) => t.orchestration?.enabled && !t.orchestration.runId)
      .sort(
        (a, b) =>
          (a.orchestration?.scheduledAt ?? '').localeCompare(b.orchestration?.scheduledAt ?? '') ||
          (a.orchestration?.executionOrder ?? 1e6) - (b.orchestration?.executionOrder ?? 1e6),
      ),
  );
  const waiting = $derived(
    scope.activeTasks.filter((t) => t.state !== 'done' && (t.orchestration?.dependencies ?? []).some((id) => scope.tasks.find((d) => d.id === id && d.state !== 'review' && d.state !== 'done'))),
  );
  function eventsOn(day: string) {
    const evs = scope.activeTasks.filter((t) => scheduledIn(t).startsWith(day)).map((t) => ({ start: minuteOf(scheduledIn(t)), item: t }));
    return lanes(evs, EVENT_MIN);
  }

  // ---- concurrency: what the project lets run at once, and what runs now ----
  let limit = $state(0);
  $effect(() => {
    let live = true;
    api.orchestrationSettings(project.id).then(
      (s) => live && (limit = s.concurrencyLimit),
      () => {},
    );
    return () => (live = false);
  });
  const running = $derived(scope.activeTasks.filter(active).length);

  // ---- scheduling ----
  let busy = $state(false);
  let picked = $state<Task | null>(null);

  async function schedule(task: Task, day: string, time: string) {
    if (busy) return;
    busy = true;
    try {
      const scheduledAt = instantFromWall(`${day}T${time}`, zone);
      const o = editableOrchestration(task.orchestration ?? emptyOrchestration());
      scope.upsertTask(await api.editTask(task, { orchestration: { ...o, enabled: true, scheduledAt, timezone: zone } }));
      app.notify(`“${task.title}” runs ${new Date(`${day}T12:00Z`).toLocaleDateString([], { weekday: 'short', timeZone: 'UTC' })} ${time}.`, 4000);
      picked = null;
    } catch (e) {
      app.notify(e instanceof Error ? e.message : String(e));
      await scope.load().catch(() => {});
    } finally {
      busy = false;
    }
  }

  async function unschedule(task: Task) {
    if (busy) return;
    busy = true;
    try {
      const o = editableOrchestration(task.orchestration ?? emptyOrchestration());
      scope.upsertTask(await api.editTask(task, { orchestration: { ...o, enabled: false, scheduledAt: undefined } }));
      app.notify(`“${task.title}” is no longer scheduled.`, 4000);
    } catch (e) {
      app.notify(e instanceof Error ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  // ---- drag and drop, and click to place ----
  let dragId = $state('');
  let dragOffset = 0;
  let ghost = $state<{ day: string; minute: number } | null>(null);

  function minuteAt(e: MouseEvent, col: HTMLElement, offset = 0) {
    const r = col.getBoundingClientRect();
    return ((e.clientY - r.top - offset) / HOUR) * 60;
  }
  function onDragStart(e: DragEvent, task: Task) {
    dragId = task.id;
    const el = e.currentTarget as HTMLElement;
    dragOffset = el.classList.contains('ev') ? e.clientY - el.getBoundingClientRect().top : 0;
    e.dataTransfer?.setData('text/plain', task.id);
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  }
  function onDragOver(e: DragEvent, day: string) {
    if (!dragId) return;
    e.preventDefault();
    const [h, m] = clockAt(minuteAt(e, e.currentTarget as HTMLElement, dragOffset)).split(':').map(Number);
    ghost = { day, minute: h * 60 + m };
  }
  function onDrop(e: DragEvent, day: string) {
    e.preventDefault();
    const task = scope.activeTasks.find((t) => t.id === dragId);
    const time = clockAt(minuteAt(e, e.currentTarget as HTMLElement, dragOffset));
    dragId = '';
    ghost = null;
    if (task) void schedule(task, day, time);
  }
  function onColumnClick(e: MouseEvent, day: string) {
    if (!picked || (e.target as HTMLElement).closest('.ev')) return;
    void schedule(picked, day, clockAt(minuteAt(e, e.currentTarget as HTMLElement)));
  }

  // Opens at the morning, or at the first thing planned this week.
  let grid = $state<HTMLElement>();
  $effect(() => {
    void days.join();
    const el = grid;
    if (!el) return;
    untrack(() => {
      const firsts = days.flatMap((d) => eventsOn(d).map((x) => x.start));
      const start = Math.min(7 * 60, ...firsts, days.includes(today) ? nowMinute - 60 : Infinity);
      el.scrollTop = Math.max(0, (start / 60) * HOUR - 8);
    });
  });

  const dayHead = (d: string) => ({
    dow: new Intl.DateTimeFormat('en', { weekday: 'short', timeZone: 'UTC' }).format(new Date(`${d}T12:00Z`)),
    n: Number(d.slice(8)),
  });
  const weekend = (d: string) => [0, 6].includes(new Date(`${d}T12:00Z`).getUTCDay());

  function saveZone() {
    const z = zoneDraft.trim();
    if (!zoneValid(z)) {
      app.notify('That is not a time zone. Use a name such as Europe/Madrid.');
      return;
    }
    zone = z;
    editingZone = false;
  }
</script>

<div class="cal">
  <div class="bar">
    <div class="nav">
      <button class="btn" onclick={() => (anchor = today)}>Today</button>
      <button class="btn icon" aria-label="Previous {view}" onclick={() => (anchor = addDays(anchor, -step))}><Icon name="left" /></button>
      <button class="btn icon" aria-label="Next {view}" onclick={() => (anchor = addDays(anchor, step))}><Icon name="right" /></button>
      <h2 class="title">{rangeTitle(days)}</h2>
    </div>
    <div class="tools">
      {#if picked}
        <span class="picking"><span class="dot" data-tone="work"></span>Click a time for “{picked.title}”<button class="btn quiet small" onclick={() => (picked = null)}>Cancel</button></span>
      {/if}
      {#if editingZone}
        <form
          class="zone-form"
          onsubmit={(e) => {
            e.preventDefault();
            saveZone();
          }}
        >
          <label class="visually-hidden" for="cal-zone">Time zone</label>
          <input id="cal-zone" class="input mono" bind:value={zoneDraft} placeholder="Europe/Madrid" autocomplete="off" spellcheck="false" />
          <button class="btn small primary" type="submit">Use</button>
        </form>
      {:else}
        <button class="btn small zone" onclick={() => ((zoneDraft = zone), (editingZone = true))} title="Times are shown and set in this time zone"><span class="mono">{zone}</span></button>
      {/if}
      <div class="seg" role="group" aria-label="View">
        <button aria-pressed={view === 'day'} onclick={() => (view = 'day')}>Day</button>
        <button aria-pressed={view === 'week'} onclick={() => (view = 'week')}>Week</button>
      </div>
    </div>
  </div>

  <div class="body">
    <div class="main">
      <div class="head" style:--days={days.length}>
        <div></div>
        {#each days as d (d)}
          {@const h = dayHead(d)}
          <div class="dh" class:today={d === today}><span>{h.dow}</span><i>{h.n}</i></div>
        {/each}
      </div>
      <div class="grid-scroll" bind:this={grid}>
        <div class="grid" style:--days={days.length} style:height="{24 * HOUR}px">
          <div class="hours" aria-hidden="true">
            {#each Array.from({ length: 24 }, (_, i) => i) as h (h)}<span class="hl" style:top="{h * HOUR - 7}px">{String(h).padStart(2, '0')}:00</span>{/each}
          </div>
          {#each days as d (d)}
            <!-- Clicking a time is a shortcut for the pointer; from the keyboard a task is scheduled from its own Schedule dialog. -->
            <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
            <div
              class="dcol"
              class:weekend={weekend(d)}
              class:pickable={!!picked}
              role="group"
              aria-label={rangeTitle([d])}
              ondragover={(e) => onDragOver(e, d)}
              ondragleave={() => (ghost = null)}
              ondrop={(e) => onDrop(e, d)}
              onclick={(e) => onColumnClick(e, d)}
            >
              {#if ghost && ghost.day === d}<div class="ghost" style:top="{(ghost.minute / 60) * HOUR}px" style:height="{(EVENT_MIN / 60) * HOUR}px"></div>{/if}
              {#each eventsOn(d) as e (e.item.id)}
                {@const t = e.item}
                {@const run = scope.latestRun[t.id]}
                {@const s = run ? runStatus(run) : undefined}
                {@const decision = scope.decisions.find((x) => x.taskId === t.id)}
                {@const eff = resolveFor(t.execution, project.execution, app.globalExecution)}
                <a
                  class="ev"
                  class:run={s?.tone === 'work'}
                  class:needs={s?.needsInput}
                  class:past={!!t.orchestration?.runId || !t.orchestration?.enabled}
                  href={taskHref(project.id, t.id)}
                  draggable="true"
                  ondragstart={(ev) => onDragStart(ev, t)}
                  ondragend={() => ((dragId = ''), (ghost = null))}
                  style:top="{(e.start / 60) * HOUR + 1}px"
                  style:height="{(EVENT_MIN / 60) * HOUR - 2}px"
                  style:left="calc({(e.lane / e.of) * 100}% + 3px)"
                  style:width="calc({100 / e.of}% - 6px)"
                  title="{t.title} · {decision ? `${schedulingLabels[decision.state]}: ${decision.reason}` : ''}"
                >
                  <b>{t.title}</b>
                  <span>{scheduledIn(t).slice(11)} · {s ? s.label.toLowerCase() : eff.agent ? agentLabel(app.agents, eff.agent) : decision ? schedulingLabels[decision.state].toLowerCase() : 'scheduled'}</span>
                  {#if t.orchestration?.enabled && !t.orchestration.runId}
                    <button
                      class="x"
                      aria-label="Unschedule {t.title}"
                      onclick={(ev) => {
                        ev.preventDefault();
                        ev.stopPropagation();
                        void unschedule(t);
                      }}><Icon name="close" size={12} /></button
                    >
                  {/if}
                </a>
              {/each}
              {#if d === today}<div class="now" style:top="{(nowMinute / 60) * HOUR}px" aria-hidden="true"></div>{/if}
            </div>
          {/each}
        </div>
      </div>
    </div>

    <aside class="side" aria-label="Planning">
      <section>
        <h3>Unscheduled<span class="chip">{unscheduled.length}</span></h3>
        {#if unscheduled.length}
          <ul class="items">
            {#each unscheduled as t (t.id)}
              <li class="item" class:on={picked?.id === t.id} draggable="true" ondragstart={(e) => onDragStart(e, t)} ondragend={() => ((dragId = ''), (ghost = null))}>
                <span class="grip" aria-hidden="true"><Icon name="grip" /></span>
                <button class="pbtn" aria-pressed={picked?.id === t.id} onclick={() => (picked = picked?.id === t.id ? null : t)}>
                  <span class="t">{t.title}</span>
                  <span class="mm">{t.state === 'backlog' ? 'Backlog' : t.state === 'review' ? 'Review' : 'Doing'}{t.execution.priority && t.execution.priority !== 'normal' ? ` · ${t.execution.priority}` : ''}</span>
                </button>
              </li>
            {/each}
          </ul>
          <p class="mm">Drag onto the week, or pick one and click a time.</p>
        {:else}
          <p class="empty-s">Everything has a time.</p>
        {/if}
      </section>

      {#if upcoming.length}
        <section>
          <h3>Queue<span class="chip">{upcoming.length}</span></h3>
          <ul class="queue">
            {#each upcoming as t (t.id)}
              {@const decision = scope.decisions.find((d) => d.taskId === t.id)}
              <li>
                <a href={taskHref(project.id, t.id)}>{t.orchestration?.executionOrder != null ? `${t.orchestration.executionOrder}. ` : ''}{t.title}</a>
                <p class="mm">
                  {t.orchestration?.scheduledAt ? wallTime(t.orchestration.scheduledAt, zone).replace('T', ' ') : 'when ready'}{decision ? ` · ${schedulingLabels[decision.state]}` : ''}
                </p>
                {#if decision?.reason && decision.state !== 'waiting_schedule'}<p class="why">{decision.reason}</p>{/if}
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if waiting.length}
        <section>
          <h3>Waiting on</h3>
          <ul class="queue">
            {#each waiting as t (t.id)}
              <li>
                <a href={taskHref(project.id, t.id)}>{t.title}</a>
                <p class="mm">
                  after {(t.orchestration?.dependencies ?? []).map((id) => scope.tasks.find((d) => d.id === id)?.title ?? 'a removed task').join(', ')}
                </p>
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      <section class="conc">
        <div class="conc-head"><span>Concurrency</span><span class="mm">{running} of {limit || '…'} running</span></div>
        {#if limit}
          <div class="slots" aria-hidden="true">
            {#each Array.from({ length: Math.min(limit, 12) }, (_, i) => i) as i (i)}<span class:on={i < running}></span>{/each}
          </div>
        {/if}
        <p class="mm">Set on the project. Work over the limit queues.</p>
      </section>
    </aside>
  </div>
</div>

<style>
  .cal {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 10px 16px;
    padding: 12px 24px;
  }

  .nav,
  .tools {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .title {
    margin-left: 8px;
    font-family: var(--font);
    font-size: 16px;
    font-weight: 600;
    letter-spacing: -0.01em;
    text-transform: none;
    color: var(--text);
  }

  .picking {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 12.5px;
    color: var(--accent);
  }

  .zone-form {
    display: flex;
    gap: 6px;
  }

  .zone-form .input {
    width: 13rem;
    min-height: 28px;
  }

  .zone .mono {
    font-size: 11.5px;
  }

  .body {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 280px;
  }

  .main {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 0 16px 0 24px;
  }

  .head,
  .grid {
    display: grid;
    grid-template-columns: 52px repeat(var(--days), minmax(0, 1fr));
  }

  .head {
    flex: none;
    border-bottom: 1px solid var(--border);
  }

  .dh {
    padding: 4px 0 8px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  .dh i {
    font-style: normal;
    font-family: var(--font);
    font-size: 17px;
    font-weight: 600;
    color: var(--text);
    letter-spacing: 0;
    width: 30px;
    height: 30px;
    border-radius: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .dh.today i {
    background: var(--pri-bg);
    color: var(--accent-text);
    box-shadow: var(--pri-sh);
  }

  .grid-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    scrollbar-gutter: stable;
  }

  .grid {
    position: relative;
  }

  .hours {
    position: relative;
  }

  .hl {
    position: absolute;
    right: 8px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  .hl:first-child {
    display: none;
  }

  .dcol {
    position: relative;
    border-left: 1px solid var(--border);
    background-image: repeating-linear-gradient(to bottom, transparent 0, transparent 47px, var(--border) 47px, var(--border) 48px);
    transition: background-color 0.15s;
  }

  .dcol.weekend {
    background-color: color-mix(in srgb, var(--text) 3%, transparent);
  }

  .dcol.pickable {
    cursor: copy;
  }

  .dcol.pickable:hover {
    background-color: color-mix(in srgb, var(--accent) 6%, transparent);
  }

  .ghost {
    position: absolute;
    left: 3px;
    right: 3px;
    border-radius: 5px;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    border: 1px dashed var(--accent);
    pointer-events: none;
    z-index: 1;
  }

  .ev {
    position: absolute;
    z-index: 2;
    display: block;
    padding: 4px 22px 4px 8px;
    border-radius: 5px;
    background: var(--surface);
    border: 1px solid var(--border);
    box-shadow: var(--card-sh);
    overflow: hidden;
    font-size: 12px;
    line-height: 1.3;
    color: var(--text);
    text-decoration: none;
    cursor: grab;
  }

  .ev:hover {
    border-color: color-mix(in srgb, var(--text-2) 50%, var(--border));
  }

  .ev.run {
    background: var(--tint);
    border-color: var(--accent);
    box-shadow:
      var(--card-sh),
      var(--live-ring);
  }

  .ev.needs {
    border-color: var(--warn);
  }

  .ev.past {
    opacity: 0.6;
  }

  .ev b {
    display: block;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ev span {
    display: block;
    font-family: var(--mono);
    font-size: 10.5px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .x {
    position: absolute;
    top: 2px;
    right: 2px;
    width: 20px;
    height: 20px;
    display: none;
    place-items: center;
    padding: 0;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--text-2);
  }

  .ev:hover .x,
  .ev:focus-within .x {
    display: grid;
  }

  .x:hover {
    background: var(--surface-2);
    color: var(--text);
  }

  .now {
    position: absolute;
    left: 0;
    right: 0;
    height: 2px;
    background: var(--accent);
    z-index: 3;
    pointer-events: none;
  }

  .now::before {
    content: '';
    position: absolute;
    left: -5px;
    top: -4px;
    width: 10px;
    height: 10px;
    background: var(--accent);
  }

  .side {
    min-height: 0;
    overflow-y: auto;
    border-left: 1px solid var(--border);
    padding: 6px 20px 20px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .side section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  h3 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
  }

  .items,
  .queue {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px 8px 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 7px;
    box-shadow: var(--card-sh);
    cursor: grab;
  }

  .item.on {
    border-color: var(--accent);
    box-shadow:
      var(--card-sh),
      var(--live-ring);
  }

  .grip {
    display: inline-flex;
    color: var(--text-2);
  }

  .pbtn {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 1px;
    padding: 0;
    border: 0;
    background: none;
    text-align: left;
  }

  .t {
    font-size: 13px;
    font-weight: 500;
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  .queue li {
    display: grid;
    gap: 2px;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border);
  }

  .queue a {
    font-size: 13px;
    font-weight: 500;
    color: var(--text);
    text-decoration: none;
  }

  .queue a:hover {
    text-decoration: underline;
  }

  .why {
    font-size: 12px;
    color: var(--text-2);
  }

  .empty-s {
    font-size: 12.5px;
    color: var(--text-2);
  }

  .conc {
    margin-top: auto;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: 7px;
    background: var(--surface);
    box-shadow: var(--card-sh);
  }

  .conc-head {
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    font-weight: 500;
  }

  .slots {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .slots span {
    width: 20px;
    height: 20px;
    border-radius: 3px;
    background: var(--surface-2);
    box-shadow: var(--tray-sh);
  }

  .slots span.on {
    background: var(--pri-bg);
    box-shadow: var(--pri-sh);
  }

  @media (max-width: 1100px) {
    .body {
      grid-template-columns: minmax(0, 1fr) 230px;
    }
  }

  @media (max-width: 899px) {
    .cal {
      height: auto;
    }

    .bar {
      padding: 4px 16px 12px;
    }

    .body {
      display: block;
    }

    .main {
      padding: 0 16px;
    }

    .grid-scroll {
      max-height: 60dvh;
    }

    .side {
      border-left: 0;
      border-top: 1px solid var(--border);
      margin-top: 12px;
      padding: 8px 16px 24px;
    }
  }
</style>
