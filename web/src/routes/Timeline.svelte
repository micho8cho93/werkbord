<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../lib/api';
  import { resolveFor } from '../lib/execution';
  import { filterFor } from '../lib/filters.svelte';
  import Icon from '../lib/Icon.svelte';
  import LabelChip from '../lib/LabelChip.svelte';
  import { filterActive, labelsOf, matches, modeLabel, modeOf } from '../lib/labels';
  import { taskHref } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import EditTaskSheet from '../lib/task/EditTaskSheet.svelte';
  import TaskFilterBar from '../lib/TaskFilterBar.svelte';
  import {
    PX_PER_DAY,
    ROW_HEIGHT,
    arrows,
    barsFor,
    dependencyMap,
    daysBetween,
    firstRows,
    groupTasks,
    localToday,
    spanOf,
    spanTitle,
    ticks,
    viewRange,
    warningsByTask,
    worst,
    type GroupBy,
    type Zoom,
  } from '../lib/timeline';
  import type { Project, Task, TimelineWarning } from '../lib/types';

  // The project's work placed in time: planned date ranges, milestones, grouped however suits, and the
  // dependencies between them as arrows. It shows; it never moves anything. A conflict is marked, and
  // what to do about it (move a date, drop a dependency) is yours to decide on the task.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  let zoom = $state<Zoom>('month');
  let groupBy = $state<GroupBy>('none');
  let collapsed = $state<Record<string, boolean>>({});
  let editing = $state<Task | null>(null);
  let scroller = $state<HTMLElement>();

  const today = $derived(localToday(app.now));
  const filter = $derived(filterFor(project.id));
  const filtering = $derived(filterActive(filter));
  const visible = $derived(scope.activeTasks.filter((t) => matches(t, filter)));
  const dated = $derived(visible.filter((t) => !!spanOf(t)));
  const undated = $derived(visible.filter((t) => !spanOf(t)).sort((a, b) => a.position - b.position));
  const groups = $derived(groupTasks(dated, app.labels, groupBy));

  const px = $derived(PX_PER_DAY[zoom]);
  const range = $derived(
    viewRange(
      dated.flatMap((t) => spanOf(t) ?? []),
      today,
    ),
  );
  const width = $derived((daysBetween(range.start, range.end) + 1) * px);
  const axis = $derived(ticks(range, zoom));
  const todayX = $derived(daysBetween(range.start, today) * px + px / 2);

  // ---- rows: a header for each group (when grouped), then its tasks ----
  type Row = { kind: 'group'; key: string; title: string; color?: string; count: number } | { kind: 'task'; key: string; task: Task };
  const rows = $derived.by(() => {
    const out: Row[] = [];
    for (const g of groups) {
      if (groupBy !== 'none') out.push({ kind: 'group', key: `g:${g.key}`, title: g.title, color: g.color, count: g.tasks.length });
      if (groupBy !== 'none' && collapsed[g.key]) continue;
      for (const t of g.tasks) out.push({ kind: 'task', key: `${g.key}:${t.id}`, task: t });
    }
    return out;
  });

  const bars = $derived(barsFor(dated, range, zoom));
  const rowOf = $derived(firstRows(rows.map((r) => (r.kind === 'task' ? r.task.id : null))));
  const deps = $derived(dependencyMap(dated));
  const lines = $derived(arrows(bars, rowOf, deps));

  // ---- what the controller found wrong ----
  let warnings = $state<TimelineWarning[]>([]);
  let warnError = $state('');
  const signature = $derived(scope.tasks.map((t) => `${t.id}:${t.version}`).join(','));
  $effect(() => {
    void signature;
    let live = true;
    const timer = setTimeout(
      () =>
        api.timeline(project.id).then(
          (w) => {
            if (!live) return;
            warnings = w;
            warnError = '';
          },
          (e) => live && (warnError = e instanceof Error ? e.message : String(e)),
        ),
      150,
    );
    return () => {
      live = false;
      clearTimeout(timer);
    };
  });
  const byTask = $derived(warningsByTask(warnings));
  const taskOf = (id: string) => scope.tasks.find((t) => t.id === id);

  function toneOf(t: Task): string {
    return t.state;
  }

  function scrollToToday() {
    if (scroller) scroller.scrollLeft = Math.max(0, todayX - scroller.clientWidth / 2 + 120);
  }

  // Open at today, once the chart is there; and again when the zoom changes its scale.
  $effect(() => {
    void zoom;
    void scope.loaded;
    untrack(() => queueMicrotask(scrollToToday));
  });

  // What the task would inherit: the levels below it, as the task's own page shows them.
  const below = $derived(resolveFor({}, project.execution, app.globalExecution));
  const names: Record<GroupBy, string> = { none: 'Nothing', label: 'Label', status: 'Status', mode: 'Who does it' };
</script>

<div class="view">
  <div class="bar">
    <div class="tools" role="group" aria-label="Timeline view">
      <div class="seg" role="group" aria-label="Zoom">
        {#each ['week', 'month', 'quarter'] as z (z)}
          <button aria-pressed={zoom === z} onclick={() => (zoom = z as Zoom)}>{z[0].toUpperCase() + z.slice(1)}</button>
        {/each}
      </div>
      <label class="pick" class:on={groupBy !== 'none'}>
        <span>Group</span>
        <select bind:value={groupBy} aria-label="Group work by">
          {#each Object.entries(names) as [id, name] (id)}<option value={id}>{name}</option>{/each}
        </select>
      </label>
      <button class="btn small" onclick={scrollToToday}>Today</button>
      <TaskFilterBar projectId={project.id} />
    </div>
    <ul class="legend" aria-label="Key">
      <li><span class="swatch range"></span>Planned range</li>
      <li><span class="swatch diamond"></span>Milestone</li>
      <li><span class="swatch arrow"></span>Depends on</li>
      <li><span class="swatch clash"></span>Conflict</li>
    </ul>
  </div>

  {#if warnings.length}
    <section class="warns" aria-label="Needs a look">
      <h2><Icon name="activity" size={14} />Needs a look <span class="chip">{warnings.length}</span></h2>
      <ul>
        {#each warnings as w, i (i)}
          {@const t = taskOf(w.itemId)}
          <li data-sev={w.severity}>
            <span class="sev">{w.severity === 'error' ? 'Problem' : w.severity === 'warning' ? 'Conflict' : 'Note'}</span>
            <span class="msg">{w.message}</span>
            {#if t}<button class="btn small" onclick={() => (editing = t)}>Edit “{t.title.length > 24 ? t.title.slice(0, 23) + '…' : t.title}”</button>{/if}
          </li>
        {/each}
      </ul>
      <p class="muted note">Nothing has been moved. Change a date or a dependency on the task when you decide how to resolve it.</p>
    </section>
  {:else if warnError}
    <p class="error pad" role="alert">{warnError}</p>
  {/if}

  <div class="scroller" bind:this={scroller}>
    {#if !scope.loaded}
      <p class="empty">Loading…</p>
    {:else if dated.length === 0}
      <div class="empty">
        <p>{filtering ? 'No planned work matches the filters.' : 'Nothing is planned yet.'}</p>
        <p class="muted">Give a task a start and end date (or make it a milestone) and it appears here. {undated.length ? 'Tasks without dates are listed below.' : ''}</p>
      </div>
    {:else}
      <div class="chart">
        <div class="axis-row">
          <div class="corner">Work</div>
          <div class="axis" style:width="{width}px">
            {#each axis as t (t.day)}
              <span class="tick" class:major={t.major} style:left="{t.x}px">{t.label}</span>
            {/each}
          </div>
        </div>
        <div class="body">
          <div class="names">
            {#each rows as r (r.key)}
              {#if r.kind === 'group'}
                {@const g = groups.find((x) => `g:${x.key}` === r.key)}
                <button class="grow" style:height="{ROW_HEIGHT}px" aria-expanded={!collapsed[r.key.slice(2)]} onclick={() => (collapsed[r.key.slice(2)] = !collapsed[r.key.slice(2)])}>
                  <span class="caret" class:shut={collapsed[r.key.slice(2)]}><Icon name="down" size={12} /></span>
                  {#if r.color}<span class="dot sq" style:background={r.color}></span>{/if}
                  <span class="gt">{r.title}</span>
                  <span class="chip">{g?.tasks.length ?? r.count}</span>
                </button>
              {:else}
                {@const w = byTask.get(r.task.id)}
                {@const span = spanOf(r.task)}
                <a class="nrow" style:height="{ROW_HEIGHT}px" href={taskHref(project.id, r.task.id)} data-state={toneOf(r.task)} title={r.task.title}>
                  {#if worst(w)}<span class="warn" data-sev={worst(w)} title={w?.map((x) => x.message).join('\n')} aria-label="Has {w?.length} {w?.length === 1 ? 'warning' : 'warnings'}">!</span>{/if}
                  <span class="nt">{r.task.title}</span>
                  {#if span}<span class="when">{spanTitle(span, Number(today.slice(0, 4)))}</span>{/if}
                </a>
              {/if}
            {/each}
          </div>
          <div class="plot" style:width="{width}px" style:height="{rows.length * ROW_HEIGHT}px">
            {#each axis as t (t.day)}<span class="grid" class:major={t.major} style:left="{t.x}px"></span>{/each}
            <span class="today" style:left="{todayX}px" title="Today"></span>
            {#each rows as r, i (r.key)}
              {#if r.kind === 'group'}
                <span class="gband" style:top="{i * ROW_HEIGHT}px" style:height="{ROW_HEIGHT}px"></span>
              {:else}
                {@const b = bars.get(r.task.id)}
                {@const w = byTask.get(r.task.id)}
                {#if b}
                  {#if b.milestone}
                    <a
                      class="ms"
                      href={taskHref(project.id, r.task.id)}
                      data-state={toneOf(r.task)}
                      data-sev={worst(w)}
                      style:left="{b.x + b.width / 2 - 8}px"
                      style:top="{i * ROW_HEIGHT + ROW_HEIGHT / 2 - 8}px"
                      aria-label="Milestone: {r.task.title}, {spanTitle(b.span, Number(today.slice(0, 4)))}"
                      title="{r.task.title} · {spanTitle(b.span, Number(today.slice(0, 4)))}"
                    ><span></span></a>
                    <span class="mt" style:left="{b.x + b.width / 2 + 14}px" style:top="{i * ROW_HEIGHT + 9}px">{r.task.title}</span>
                  {:else}
                    <a
                      class="rb"
                      href={taskHref(project.id, r.task.id)}
                      data-state={toneOf(r.task)}
                      data-sev={worst(w)}
                      style:left="{b.x}px"
                      style:width="{Math.max(b.width - 2, 6)}px"
                      style:top="{i * ROW_HEIGHT + 6}px"
                      style:height="{ROW_HEIGHT - 12}px"
                      title="{r.task.title} · {spanTitle(b.span, Number(today.slice(0, 4)))}"
                    ><span class="bt">{r.task.title}</span></a>
                  {/if}
                {/if}
              {/if}
            {/each}
            <svg class="links" width={width} height={rows.length * ROW_HEIGHT} aria-hidden="true">
              <defs>
                <marker id="tl-head" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0 L8 4 L0 8 z" fill="currentColor" /></marker>
              </defs>
              {#each lines as l (l.from + '>' + l.to)}
                <path d={l.d} class="link" class:clash={l.conflict} marker-end="url(#tl-head)" />
              {/each}
            </svg>
          </div>
        </div>
      </div>
    {/if}

    {#if undated.length}
      <section class="undated" aria-label="Not planned yet">
        <h2>Not planned yet <span class="chip">{undated.length}</span></h2>
        <ul>
          {#each undated as t (t.id)}
            {@const ls = labelsOf(t, app.labels)}
            <li>
              <a href={taskHref(project.id, t.id)} class="ut">{t.title}</a>
              {#each ls as l (l.id)}<LabelChip label={l} small />{/each}
              {#if modeOf(t) !== 'agent'}<span class="chip">{modeLabel(modeOf(t))}</span>{/if}
              <button class="btn small" onclick={() => (editing = t)}>Plan…</button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}
  </div>
</div>

{#if editing}<EditTaskSheet task={editing} {scope} inherited={below} onclose={() => (editing = null)} />{/if}

<style>
  .view {
    --names-w: 240px;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .bar {
    flex: none;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 10px 16px;
    padding: 10px 24px;
  }

  .tools {
    display: flex;
    align-items: flex-start;
    flex-wrap: wrap;
    gap: 8px;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: 12px;
    color: var(--text-2);
    align-self: center;
  }

  .legend li {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .swatch {
    display: inline-block;
  }

  .swatch.range {
    width: 18px;
    height: 8px;
    border-radius: 3px;
    background: var(--accent);
  }

  .swatch.diamond {
    width: 9px;
    height: 9px;
    transform: rotate(45deg);
    background: var(--text);
  }

  .swatch.arrow {
    width: 18px;
    border-top: 1.5px solid var(--text-2);
  }

  .swatch.clash {
    width: 18px;
    border-top: 1.5px solid var(--danger);
  }

  .warns {
    flex: none;
    margin: 0 24px 8px;
    padding: 10px 12px;
    border-radius: var(--radius-tray);
    border: 1px solid color-mix(in srgb, var(--warn) 50%, var(--border));
    background: color-mix(in srgb, var(--warn) 7%, var(--surface));
  }

  .warns h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 6px;
    font-size: 13px;
  }

  .warns ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
    max-height: 160px;
    overflow: auto;
  }

  .warns li {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
  }

  .sev {
    flex: none;
    font-family: var(--mono);
    font-size: 11px;
    text-transform: uppercase;
    color: var(--warn-text);
  }

  li[data-sev='error'] .sev {
    color: var(--danger-text);
  }

  li[data-sev='info'] .sev {
    color: var(--text-2);
  }

  .msg {
    flex: 1;
    min-width: 0;
  }

  .note {
    margin: 6px 0 0;
    font-size: 12px;
  }

  .pad {
    padding: 0 24px;
  }

  .scroller {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 24px 24px;
  }

  .empty {
    padding: 24px 0;
    color: var(--text-2);
  }

  .chart {
    position: relative;
    width: max-content;
    min-width: 100%;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
    overflow: clip;
  }

  .axis-row {
    display: flex;
    position: sticky;
    top: 0;
    z-index: 4;
    height: 32px;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
  }

  .corner {
    position: sticky;
    left: 0;
    z-index: 5;
    flex: none;
    width: var(--names-w);
    display: flex;
    align-items: center;
    padding: 0 12px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-2);
    background: var(--surface);
    border-right: 1px solid var(--border);
  }

  .axis {
    position: relative;
    flex: none;
    height: 100%;
  }

  .tick {
    position: absolute;
    top: 0;
    height: 100%;
    display: flex;
    align-items: center;
    padding-left: 4px;
    border-left: 1px solid var(--border);
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    white-space: nowrap;
  }

  .tick.major {
    color: var(--text);
  }

  .body {
    display: flex;
  }

  .names {
    position: sticky;
    left: 0;
    z-index: 3;
    flex: none;
    width: var(--names-w);
    background: var(--surface);
    border-right: 1px solid var(--border);
  }

  .nrow,
  .grow {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 0 12px;
    border: 0;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
    background: none;
    color: var(--text);
    text-decoration: none;
    text-align: left;
    font-size: 13px;
    overflow: hidden;
  }

  .nrow:hover {
    background: var(--bg);
  }

  .nrow[data-state='done'] .nt {
    color: var(--text-2);
    text-decoration: line-through;
    text-decoration-color: color-mix(in srgb, var(--text-2) 50%, transparent);
  }

  .nt {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .when {
    flex: none;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  .grow {
    background: var(--bg);
    font-weight: 600;
    cursor: pointer;
  }

  .caret {
    display: inline-flex;
    transition: transform 0.15s var(--ease);
  }

  .caret.shut {
    transform: rotate(-90deg);
  }

  .gt {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sq {
    width: 10px;
    height: 10px;
    border-radius: 3px;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.15);
  }

  .warn {
    flex: none;
    width: 16px;
    height: 16px;
    display: inline-grid;
    place-items: center;
    border-radius: 50%;
    font-size: 11px;
    font-weight: 700;
    color: #fff;
    background: var(--warn);
  }

  .warn[data-sev='error'] {
    background: var(--danger);
  }

  .warn[data-sev='info'] {
    background: var(--text-2);
  }

  .plot {
    position: relative;
    flex: none;
  }

  .grid {
    position: absolute;
    top: 0;
    bottom: 0;
    border-left: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }

  .grid.major {
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

  .gband {
    position: absolute;
    left: 0;
    right: 0;
    background: var(--bg);
    opacity: 0.8;
  }

  .rb {
    position: absolute;
    z-index: 2;
    display: flex;
    align-items: center;
    padding: 0 8px;
    border-radius: 5px;
    background: color-mix(in srgb, var(--text-2) 55%, var(--surface));
    color: #fff;
    font-size: 12px;
    text-decoration: none;
    overflow: hidden;
    white-space: nowrap;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.12);
  }

  .rb[data-state='doing'] {
    background: var(--accent);
    color: var(--accent-text);
  }

  .rb[data-state='review'] {
    background: var(--amber);
    color: #14130f;
  }

  .rb[data-state='done'] {
    background: var(--ok);
    opacity: 0.8;
  }

  .rb[data-sev='error'],
  .ms[data-sev='error'] {
    outline: 2px solid var(--danger);
    outline-offset: 1px;
  }

  .rb[data-sev='warning'],
  .ms[data-sev='warning'] {
    outline: 2px solid var(--warn);
    outline-offset: 1px;
  }

  .rb:focus-visible,
  .ms:focus-visible {
    outline: 2px solid var(--text);
    outline-offset: 2px;
  }

  .bt {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .ms {
    position: absolute;
    z-index: 2;
    width: 16px;
    height: 16px;
    display: grid;
    place-items: center;
  }

  .ms span {
    width: 12px;
    height: 12px;
    transform: rotate(45deg);
    background: var(--text);
    border-radius: 2px;
  }

  .ms[data-state='done'] span {
    background: var(--ok);
  }

  .ms[data-state='doing'] span {
    background: var(--accent);
  }

  .mt {
    position: absolute;
    z-index: 2;
    font-size: 12px;
    white-space: nowrap;
    color: var(--text);
    pointer-events: none;
  }

  .links {
    position: absolute;
    inset: 0;
    z-index: 1;
    pointer-events: none;
    overflow: visible;
    color: var(--text-2);
  }

  .link {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
  }

  .link.clash {
    stroke: var(--danger);
    color: var(--danger);
  }

  .undated {
    margin-top: 16px;
  }

  .undated h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }

  .undated ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }

  .undated li {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    min-height: 40px;
    border-top: 1px solid var(--border);
  }

  .ut {
    flex: 1 1 14rem;
    min-width: 0;
    color: var(--text);
    text-decoration: none;
    font-size: 13px;
  }

  .ut:hover {
    text-decoration: underline;
  }

  @media (max-width: 899px) {
    .view {
      --names-w: 150px;
    }

    .bar,
    .scroller {
      padding-left: 16px;
      padding-right: 16px;
    }

    .warns {
      margin: 0 16px 8px;
    }

    .when {
      display: none;
    }

    .legend {
      display: none;
    }
  }
</style>
