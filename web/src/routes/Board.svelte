<script lang="ts">
  import { untrack } from 'svelte';
  import { readDraft, writeDraft, uncertainMutation } from '../lib/recovery';
  import { ApiError, api } from '../lib/api';
  import { agentWorkable, dropAction, firstNeedsYouColumn } from '../lib/board';
  import Archive from '../lib/board/Archive.svelte';
  import TaskCard from '../lib/board/TaskCard.svelte';
  import { agentLabel, resolveFor, type Resolved } from '../lib/execution';
  import { filterFor, setFilter } from '../lib/filters.svelte';
  import { filterActive, matches, noFilter } from '../lib/labels';
  import TaskFilterBar from '../lib/TaskFilterBar.svelte';
  import { runStatus } from '../lib/format';
  import GitSheets from '../lib/git/GitSheets.svelte';
  import { sheets } from '../lib/git/sheets.svelte';
  import { gitStore } from '../lib/git/store.svelte';
  import Icon from '../lib/Icon.svelte';
  import { router } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import { TASK_STATES, TASK_STATE_LABELS, type GitBranch, type Priority, type Project, type Task, type TaskState } from '../lib/types';

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  // Branches, for the review column: what can be merged from the card. Loaded while the board is open.
  const git = $derived(gitStore(project.id));
  $effect(() => git.watch());

  // ---- filters ----
  let filtersOpen = $state(false);
  let viewingArchive = $state(false);
  let agentFilter = $state('');
  let runnerFilter = $state('');
  let priorityFilter = $state<Priority | ''>('');
  const labelFilter = $derived(filterFor(project.id));
  const activeFilters = $derived(Number(!!agentFilter) + Number(!!runnerFilter) + Number(!!priorityFilter) + labelFilter.labels.length + Number(!!labelFilter.mode) + Number(viewingArchive));
  const filtering = $derived(!!(agentFilter || runnerFilter || priorityFilter) || filterActive(labelFilter));

  const effectiveOf = (t: Task): Resolved => resolveFor(t.execution, project.execution, app.globalExecution);

  function shown(t: Task): boolean {
    if (!filtering) return true;
    if (!matches(t, labelFilter)) return false;
    const run = scope.latestRun[t.id];
    const eff = effectiveOf(t);
    if (agentFilter && (run?.agentId ?? eff.agent) !== agentFilter) return false;
    if (runnerFilter && (run?.runnerId ?? eff.runner) !== runnerFilter) return false;
    if (priorityFilter && eff.priority !== priorityFilter) return false;
    return true;
  }

  const DONE_LIMIT = 20;
  let showAllDone = $state(false);

  const columns = $derived(
    TASK_STATES.map((state) => {
      const all = scope.activeTasks.filter((t) => t.state === state && shown(t));
      const sorted =
        state === 'done' ? all.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)) : all.sort((a, b) => a.position - b.position);
      const limited = state === 'done' && !showAllDone ? sorted.slice(0, DONE_LIMIT) : sorted;
      return { state, tasks: limited, total: sorted.length };
    }),
  );

  // On a phone the columns are wider than the screen, so the board opens on the first one that needs you (a question,
  // a reply, finished work to review). Once, when the board has loaded; after that the person's own panning rules.
  let cols = $state<HTMLElement>();
  let opened = false;
  $effect(() => {
    if (opened || !scope.loaded || !cols || !window.matchMedia('(max-width: 899px)').matches) return;
    opened = true;
    if (router.taskId) return;
    const state = firstNeedsYouColumn(scope.activeTasks, scope.latestRun, (r) => scope.pendingFor(r.id).length > 0);
    const col = state && cols.querySelector<HTMLElement>(`#col-${state}`);
    if (col) cols.scrollTo({ left: col.getBoundingClientRect().left - cols.getBoundingClientRect().left + cols.scrollLeft - 16 });
  });

  // ---- the legend: what the agents are doing, at a glance ----
  const legend = $derived.by(() => {
    let running = 0,
      needs = 0,
      blocked = 0,
      ready = 0;
    for (const t of scope.activeTasks) {
      const r = scope.latestRun[t.id];
      const s = r ? runStatus(r) : undefined;
      if (s?.tone === 'work') running++;
      else if (s?.tone === 'ask' || s?.tone === 'idle') needs++;
      else if (s?.tone === 'block') blocked++;
      if (t.state === 'review' && !s?.active) ready++;
    }
    return [
      { tone: 'work', n: running, label: 'running' },
      { tone: 'ask', n: needs, label: 'needs you' },
      { tone: 'block', n: blocked, label: 'blocked' },
      { tone: 'ok', n: ready, label: 'ready for review' },
    ];
  });

  // ---- what can be merged from the board: the task's own branch, while it is not merged yet ----
  function branchOf(task: Task): GitBranch | undefined {
    const run = scope.latestRun[task.id];
    return git.overview?.branches.find(
      (b) => b.scope === 'local' && !b.target && (b.devboard.taskId === task.id || (!!run?.branch && b.name === run.branch)),
    );
  }

  function mergeable(task: Task): boolean {
    const run = scope.latestRun[task.id];
    if (run?.remote) return false;
    const b = branchOf(task);
    return !!b && !b.merged && b.vsTarget.ahead > 0;
  }

  let clearingDone = $state(false);
  let pendingTasks = $state<Record<string, boolean>>({});
  async function clearDone() {
    if (clearingDone) return;
    clearingDone = true;
    try { const tasks = await api.archiveDone(project.id); tasks.forEach(t => scope.upsertTask(t)); app.notify(`${tasks.length} tasks moved to the archive.`); }
    catch (e) { app.handleError(e); await scope.load().catch(e => app.handleError(e)); }
    finally { clearingDone = false; }
  }

  // ---- actions ----
  async function move(task: Task, state: TaskState) {
    if (pendingTasks[task.id]) return;
    pendingTasks[task.id] = true;
    try {
      scope.upsertTask(await api.moveTask(task, state));
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        app.notify('This task changed on another device. The board has been refreshed.');
        await scope.load().catch((e) => app.handleError(e));
      } else app.notify(err instanceof Error ? err.message : String(err));
    } finally { pendingTasks[task.id] = false; }
  }

  async function start(task: Task) {
    if (pendingTasks[task.id]) return;
    pendingTasks[task.id] = true;
    try {
      const r = await api.startRun(project.id, task.id);
      scope.upsertRun(r);
      // Reconcile the persisted card before enabling another drop.
      await scope.load();
      const current = scope.activeTasks.find(t => t.id === task.id);
      if (current?.state === 'backlog' && runStatus(r).active) {
        try { scope.upsertTask(await api.moveTask(current, 'doing')); }
        catch (e) {
          if (!(e instanceof ApiError && e.code === 'conflict')) throw e;
          await scope.load();
          if (scope.activeTasks.find(t => t.id === task.id)?.state !== 'doing') throw e;
        }
      }
      app.notify(`${agentLabel(app.agents, r.agentId)} started on “${task.title}”.`, 4000);
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    } finally { pendingTasks[task.id] = false; }
  }

  async function merge(task: Task) {
    if (!git.overview) await git.load();
    const b = branchOf(task);
    if (!b) {
      app.notify('This task has no branch to merge.');
      return;
    }
    sheets.open({ kind: 'merge', branch: b.name, onmerged: () => void move(task, 'done') });
  }

  // ---- drag and drop: the rules of the work, not of the furniture ----
  let dragging = $state<Task | null>(null);
  let over = $state<TaskState | ''>('');

  function onDragStart(e: DragEvent, task: Task) {
    if (pendingTasks[task.id]) { e.preventDefault(); return; }
    dragging = task;
    e.dataTransfer?.setData('text/plain', task.id);
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  }

  function onDragEnd() {
    dragging = null;
    over = '';
  }

  function onDragOver(e: DragEvent, state: TaskState) {
    if (!dragging) return;
    e.preventDefault();
    over = state;
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
  }

  function onDrop(e: DragEvent, state: TaskState) {
    e.preventDefault();
    const id = e.dataTransfer?.getData('text/plain');
    const task = dragging ?? scope.activeTasks.find(t => t.id === id);
    onDragEnd();
    if (!task) return;
    const run = scope.latestRun[task.id];
    switch (dropAction(task, run, state, mergeable(task), agentWorkable(task, project))) {
      case 'start':
        void start(task);
        break;
      case 'merge':
        void merge(task);
        break;
      case 'move':
        void move(task, state);
        break;
    }
  }

  /** What a drop on a column would do, said on the column while dragging. */
  function dropHint(state: TaskState): string {
    if (!dragging || state === dragging.state) return '';
    const run = scope.latestRun[dragging.id];
    const a = dropAction(dragging, run, state, mergeable(dragging), agentWorkable(dragging, project));
    return a === 'start' ? 'Drop to start an agent' : a === 'merge' ? 'Drop to merge' : `Move to ${TASK_STATE_LABELS[state]}`;
  }

  // ---- quick add, in Backlog ----
  const quickKey = untrack(() => `quick-task.${project.id}`);
  const quickDraft = untrack(() => readDraft<{ title: string; uncertain: boolean }>(quickKey));
  let adding = $state(!!quickDraft?.title);
  let newTitle = $state(quickDraft?.title ?? '');
  let addUncertain = $state(quickDraft?.uncertain ?? false);
  let addError = $state('');
  let quickStored = $state(true);
  function persistQuick() { quickStored = writeDraft(quickKey, { title: newTitle, uncertain: addUncertain }); }
  $effect(() => { void newTitle; void addUncertain; untrack(persistQuick); });
  let addBusy = $state(false);
  let addInput = $state<HTMLInputElement>();

  $effect(() => {
    if (adding) untrack(() => addInput?.focus());
  });

  async function quickAdd(e: SubmitEvent) {
    e.preventDefault();
    const title = newTitle.trim();
    if (!title || addBusy || addUncertain) return;
    addBusy = true;
    addError = '';
    addUncertain = true;
    persistQuick();
    try {
      scope.upsertTask(await api.createTask(project.id, title));
      newTitle = '';
      addUncertain = false;
      writeDraft(quickKey, null);
    } catch (err) {
      addUncertain = uncertainMutation(err);
      addError = addUncertain ? 'Save status is unknown. Reconnect and check Backlog before adding this task again.' : `${err instanceof Error ? err.message : String(err)} Correct the title, then try Add again.`;
      persistQuick();
    } finally {
      addBusy = false;
      addInput?.focus();
    }
  }
</script>

<svelte:window onpagehide={persistQuick} />

<div class="view">
  <div class="bar">
    <button class="btn phone-filters" type="button" aria-expanded={filtersOpen} aria-controls="board-filters" onclick={() => filtersOpen = !filtersOpen}>Filters <span class="chip">{activeFilters}</span></button>
    <div class="filters" id="board-filters" class:expanded={filtersOpen} role="group" aria-label="Filter the board">
      {#if app.agents.length > 1 && project.kind !== 'work'}
        <label class="pick" class:on={agentFilter}>
          <span>Agent</span>
          <select bind:value={agentFilter} aria-label="Filter by agent">
            <option value="">any</option>
            {#each app.agents as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
          </select>
        </label>
      {/if}
      {#if app.runners.length > 1 && project.kind !== 'work'}
        <label class="pick" class:on={runnerFilter}>
          <span>Runner</span>
          <select bind:value={runnerFilter} aria-label="Filter by runner">
            <option value="">any</option>
            {#each app.runners as r (r.id)}<option value={r.id}>{r.kind === 'local' ? 'this computer' : r.name}</option>{/each}
          </select>
        </label>
      {/if}
      <label class="pick" class:on={priorityFilter}>
        <span>Priority</span>
        <select bind:value={priorityFilter} aria-label="Filter by priority">
          <option value="">any</option>
          <option value="high">high</option>
          <option value="normal">normal</option>
          <option value="low">low</option>
        </select>
      </label>
      <TaskFilterBar projectId={project.id} />
      <button class="btn small" aria-pressed={viewingArchive} onclick={() => viewingArchive = !viewingArchive}>{viewingArchive ? 'Back to board' : `Archive (${scope.archivedTasks.length})`}</button>
      <button class="btn small" disabled={clearingDone || !scope.activeTasks.some(t => t.state === 'done')} onclick={clearDone} title="Move all Done tasks into the archive and keep their details">{clearingDone ? 'Clearing…' : 'Clear Done'}</button>
      {#if filtering}
        <button class="btn quiet small" onclick={() => ((agentFilter = ''), (runnerFilter = ''), (priorityFilter = ''), setFilter(project.id, noFilter()))}>Clear all</button>
      {/if}
    </div>
    <!-- What agents are doing: a work project has none. -->
    {#if project.kind !== 'work'}
      <ul class="legend" aria-label="Agents at a glance">
        {#each legend as l (l.tone)}
          <li class:zero={l.n === 0}><span class="dot" data-tone={l.tone}></span>{l.n} {l.label}</li>
        {/each}
      </ul>
    {/if}
  </div>

  {#if viewingArchive}
    <Archive {scope} />
  {:else}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex (The scrollable board needs keyboard focus for horizontal panning.) -->
  <div class="cols" role="region" aria-label="Kanban board" tabindex="0" bind:this={cols}>
    {#each columns as col (col.state)}
      {@const hint = dropHint(col.state)}
      <section
        id="col-{col.state}"
        class="col tray"
        class:over={over === col.state && !!hint}
        class:target={!!hint}
        aria-label={TASK_STATE_LABELS[col.state]}
        ondragover={(e) => onDragOver(e, col.state)}
        ondragleave={(e) => { if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) over = over === col.state ? '' : over; }}
        ondrop={(e) => onDrop(e, col.state)}
      >
        <header class="ch">
          <h2>{TASK_STATE_LABELS[col.state]}</h2>
          <span class="chip">{col.total}</span>
          {#if col.state === 'backlog'}
            <button class="btn quiet small icon add" onclick={() => (adding = !adding)} aria-label="Add a task to Backlog" aria-expanded={adding} title="Add a task">
              <Icon name="plus" />
            </button>
          {/if}
        </header>

        {#if hint}<p class="drop-hint" aria-live="polite">{hint}</p>{/if}

        {#if col.state === 'backlog' && adding}
          <form class="quick" onsubmit={quickAdd}>
            <label class="visually-hidden" for="quick-add">New task title</label>
            <input
              id="quick-add"
              disabled={addBusy || addUncertain}
              class="input"
              placeholder="Task title, then Enter"
              maxlength="200"
              bind:this={addInput}
              bind:value={newTitle}
              onkeydown={(e) => {
                if (e.key === 'Escape') {
                  adding = false;
                }
              }}
            />
            {#if addError}<p class="error" role="alert">{addError}</p>{/if}
            {#if addUncertain && !addError}<p role="status">Save status is unknown. Reconnect and check Backlog before adding again.</p>{/if}
            {#if !quickStored}<p role="status">Copy this title before reloading; browser draft storage is unavailable.</p>{/if}
            <div class="quick-row">
              <button class="btn small primary" type="submit" disabled={addBusy || addUncertain || !newTitle.trim()}>Add</button>
              <button class="btn small quiet" type="button" disabled={addUncertain} onclick={() => { app.newTaskOpen = true; adding = false; }}>More options…</button>
            </div>
          </form>
        {/if}

        <ul class="list">
          {#each col.tasks as task (task.id)}
            {@const run = scope.latestRun[task.id]}
            <TaskCard
              {task}
              {run}
              pending={!!pendingTasks[task.id]}
              {scope}
              effective={effectiveOf(task)}
              selected={router.taskId === task.id}
              mergeable={mergeable(task)}
              agentWork={agentWorkable(task, project)}
              work={project.kind === 'work'}
              branch={branchOf(task)}
              onstart={start}
              onmerge={merge}
              onmove={move}
              ondragstart={onDragStart}
              ondragend={onDragEnd}
            />
          {:else}
            <li class="none">
              {#if !scope.loaded}Loading…
              {:else if filtering}Nothing matches the filters.
              {:else if col.state === 'backlog'}Nothing waiting. Add a task with <kbd class="key">N</kbd>.
              {:else if col.state === 'doing'}{project.kind === 'work' ? 'Nothing is under way.' : 'No agent is working. Start one from Backlog.'}
              {:else if col.state === 'review'}{project.kind === 'work' ? 'Work that needs a second look.' : 'Finished work lands here for you to review.'}
              {:else}Merged work lands here.{/if}
            </li>
          {/each}
          {#if col.state === 'done' && col.total > DONE_LIMIT}
            <li class="more"><button class="btn quiet small" onclick={() => (showAllDone = !showAllDone)}>{showAllDone ? 'Show recent only' : `Show all ${col.total}`}</button></li>
          {/if}
        </ul>
      </section>
    {/each}
  </div>

  {/if}
  <button class="fab" type="button" onclick={() => (app.newTaskOpen = true)} aria-label="New task"><Icon name="plus" size={22} /></button>
</div>

<GitSheets projectId={project.id} store={git} />

<style>
  .phone-filters { display: none; }
  .view {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 10px 16px;
    min-height: 56px;
    padding: 10px 24px;
  }

  .filters {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 18px;
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: 13px;
    color: var(--text-2);
  }

  .legend li {
    display: inline-flex;
    align-items: center;
    gap: 7px;
  }

  /* Nothing there: the dot steps back, the words stay at the secondary text colour (dimming them too put them under 4.5:1). */
  .legend .zero .dot {
    opacity: 0.45;
  }

  .cols {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: grid;
    grid-template-columns: repeat(4, minmax(240px, 1fr));
    gap: 16px;
    padding: 0 24px 24px;
    overflow-x: auto;
    scroll-padding-inline: 24px;
    overscroll-behavior-x: contain;
  }

  .col {
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
    padding: 10px 6px 6px 10px;
    transition: box-shadow 0.15s;
  }

  .col.target {
    box-shadow:
      var(--tray-sh),
      0 0 0 1px color-mix(in srgb, var(--accent) 30%, transparent);
  }

  .col.over {
    box-shadow:
      var(--tray-sh),
      0 0 0 2px var(--accent);
  }

  .ch {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 28px;
    padding: 0 4px 8px 4px;
  }

  .ch h2 {
    font-family: var(--font);
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0;
    text-transform: none;
    color: var(--text);
  }

  .add {
    margin-left: auto;
    margin-right: 4px;
  }

  .drop-hint {
    flex: none;
    margin: 0 4px 8px 0;
    padding: 6px 8px;
    border: 1px dashed var(--accent);
    border-radius: 6px;
    font-family: var(--mono);
    font-size: 11.5px;
    color: var(--accent);
    text-align: center;
  }

  .quick {
    flex: none;
    display: grid;
    gap: 8px;
    margin: 0 4px 10px 0;
  }

  .quick .input {
    background: var(--surface);
  }

  .quick-row {
    display: flex;
    gap: 6px;
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    list-style: none;
    margin: 0;
    padding: 2px 4px 6px 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
    scrollbar-gutter: stable;
  }

  .none {
    padding: 18px 8px;
    text-align: center;
    font-family: var(--mono);
    font-size: 11.5px;
    line-height: 1.6;
    color: var(--text-2);
  }

  .more {
    display: flex;
    justify-content: center;
  }

  .fab {
    display: none;
  }

  /* Keep the workflow side by side; narrow screens pan across the same board. */
  @media (max-width: 899px) {
    .bar {
      padding: 4px 16px 8px;
      min-height: 0;
    }

    .legend {
      font-size: 13px;
      gap: 6px 14px;
    }

    .phone-filters { display: inline-flex; align-items: center; gap: 8px; min-height: 44px; }
    .filters { display: none; width: 100%; min-width: 0; }
    .filters.expanded { display: flex; }
    .filters :global(select) { max-width: 100%; }
    .bar { align-items: flex-start; }
    .legend { width: 100%; }

    .view {
      height: auto;
    }

    .cols {
      flex: none;
      height: max(360px, calc(100dvh - 280px));
      grid-template-columns: repeat(4, minmax(min(80vw, 320px), 1fr));
      padding: 0 16px 16px;
      margin-bottom: 80px;
      scroll-padding-inline: 16px;
      scroll-snap-type: x proximity;
    }

    .col { scroll-snap-align: start; }

    .fab {
      position: fixed;
      right: 16px;
      bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 16px);
      z-index: 6;
      display: grid;
      place-items: center;
      width: 56px;
      height: 56px;
      border-radius: 10px;
      border: 1px solid var(--pri-bd);
      background: var(--pri-bg);
      box-shadow: var(--pri-sh);
      color: var(--accent-text);
    }
  }
</style>
