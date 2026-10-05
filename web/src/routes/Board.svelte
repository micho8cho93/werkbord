<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError, api } from '../lib/api';
  import { dropAction } from '../lib/board';
  import TaskCard from '../lib/board/TaskCard.svelte';
  import { agentLabel, resolveFor, type Resolved } from '../lib/execution';
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
  let agentFilter = $state('');
  let runnerFilter = $state('');
  let priorityFilter = $state<Priority | ''>('');
  const filtering = $derived(!!(agentFilter || runnerFilter || priorityFilter));

  const effectiveOf = (t: Task): Resolved => resolveFor(t.execution, project.execution, app.globalExecution);

  function shown(t: Task): boolean {
    if (!filtering) return true;
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
      const all = scope.tasks.filter((t) => t.state === state && shown(t));
      const sorted =
        state === 'done' ? all.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)) : all.sort((a, b) => a.position - b.position);
      const limited = state === 'done' && !showAllDone ? sorted.slice(0, DONE_LIMIT) : sorted;
      return { state, tasks: limited, total: sorted.length };
    }),
  );

  // ---- the legend: what the agents are doing, at a glance ----
  const legend = $derived.by(() => {
    let running = 0,
      needs = 0,
      blocked = 0,
      ready = 0;
    for (const t of scope.tasks) {
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

  // ---- actions ----
  async function move(task: Task, state: TaskState) {
    try {
      scope.upsertTask(await api.moveTask(task, state));
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        app.notify('This task changed on another device. The board has been refreshed.');
        await scope.load().catch((e) => app.handleError(e));
      } else app.notify(err instanceof Error ? err.message : String(err));
    }
  }

  async function start(task: Task) {
    try {
      const r = await api.startRun(project.id, task.id);
      scope.upsertRun(r);
      app.notify(`${agentLabel(app.agents, r.agentId)} started on “${task.title}”.`, 4000);
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    }
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
  }

  function onDrop(e: DragEvent, state: TaskState) {
    e.preventDefault();
    const task = dragging;
    onDragEnd();
    if (!task) return;
    const run = scope.latestRun[task.id];
    switch (dropAction(task, run, state, mergeable(task))) {
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
    const a = dropAction(dragging, run, state, mergeable(dragging));
    return a === 'start' ? 'Drop to start an agent' : a === 'merge' ? 'Drop to merge' : `Move to ${TASK_STATE_LABELS[state]}`;
  }

  // ---- quick add, in Backlog ----
  let adding = $state(false);
  let newTitle = $state('');
  let addBusy = $state(false);
  let addInput = $state<HTMLInputElement>();

  $effect(() => {
    if (adding) untrack(() => addInput?.focus());
  });

  async function quickAdd(e: SubmitEvent) {
    e.preventDefault();
    const title = newTitle.trim();
    if (!title || addBusy) return;
    addBusy = true;
    try {
      scope.upsertTask(await api.createTask(project.id, title));
      newTitle = '';
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    } finally {
      addBusy = false;
      addInput?.focus();
    }
  }

  // On a phone one column shows at a time.
  let activeColumn = $state<TaskState>('doing');
  $effect(() => {
    // Start on the column where things are happening; Backlog when nothing is.
    untrack(() => {
      if (scope.tasks.length && !scope.tasks.some((t) => t.state === 'doing')) activeColumn = 'backlog';
    });
  });
</script>

<div class="view">
  <div class="bar">
    <div class="filters" role="group" aria-label="Filter the board">
      {#if app.agents.length > 1}
        <label class="filter" class:on={agentFilter}>
          <span>Agent</span>
          <select bind:value={agentFilter} aria-label="Filter by agent">
            <option value="">any</option>
            {#each app.agents as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
          </select>
        </label>
      {/if}
      {#if app.runners.length > 1}
        <label class="filter" class:on={runnerFilter}>
          <span>Runner</span>
          <select bind:value={runnerFilter} aria-label="Filter by runner">
            <option value="">any</option>
            {#each app.runners as r (r.id)}<option value={r.id}>{r.kind === 'local' ? 'this computer' : r.name}</option>{/each}
          </select>
        </label>
      {/if}
      <label class="filter" class:on={priorityFilter}>
        <span>Priority</span>
        <select bind:value={priorityFilter} aria-label="Filter by priority">
          <option value="">any</option>
          <option value="high">high</option>
          <option value="normal">normal</option>
          <option value="low">low</option>
        </select>
      </label>
      {#if filtering}
        <button class="btn quiet small" onclick={() => ((agentFilter = ''), (runnerFilter = ''), (priorityFilter = ''))}>Clear</button>
      {/if}
    </div>
    <ul class="legend" aria-label="Agents at a glance">
      {#each legend as l (l.tone)}
        <li class:zero={l.n === 0}><span class="dot" data-tone={l.tone}></span>{l.n} {l.label}</li>
      {/each}
    </ul>
  </div>

  <div class="pills" role="tablist" aria-label="Columns">
    {#each columns as col (col.state)}
      <button role="tab" class="pill" aria-selected={activeColumn === col.state} aria-controls="col-{col.state}" onclick={() => (activeColumn = col.state)}>
        {TASK_STATE_LABELS[col.state]}<span class="mono">{col.total}</span>
      </button>
    {/each}
  </div>

  <div class="cols">
    {#each columns as col (col.state)}
      {@const hint = dropHint(col.state)}
      <section
        id="col-{col.state}"
        class="col tray"
        class:over={over === col.state && !!hint}
        class:target={!!hint}
        data-active={activeColumn === col.state}
        aria-label={TASK_STATE_LABELS[col.state]}
        ondragover={(e) => onDragOver(e, col.state)}
        ondragleave={() => (over = over === col.state ? '' : over)}
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
              class="input"
              placeholder="Task title, then Enter"
              maxlength="200"
              bind:this={addInput}
              bind:value={newTitle}
              onkeydown={(e) => {
                if (e.key === 'Escape') {
                  adding = false;
                  newTitle = '';
                }
              }}
            />
            <div class="quick-row">
              <button class="btn small primary" type="submit" disabled={addBusy || !newTitle.trim()}>Add</button>
              <button class="btn small quiet" type="button" onclick={() => ((app.newTaskOpen = true), (adding = false))}>More options…</button>
            </div>
          </form>
        {/if}

        <ul class="list">
          {#each col.tasks as task (task.id)}
            {@const run = scope.latestRun[task.id]}
            <TaskCard
              {task}
              {run}
              {scope}
              effective={effectiveOf(task)}
              selected={router.taskId === task.id}
              mergeable={mergeable(task)}
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
              {:else if col.state === 'doing'}No agent is working. Start one from Backlog.
              {:else if col.state === 'review'}Finished work lands here for you to review.
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

  <button class="fab" type="button" onclick={() => (app.newTaskOpen = true)} aria-label="New task"><Icon name="plus" size={22} /></button>
</div>

<GitSheets projectId={project.id} store={git} />

<style>
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

  /* A filter is a raised button that is also a picker. */
  .filter {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 28px;
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--btn-bg);
    box-shadow: var(--btn-sh);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
  }

  .filter span {
    color: var(--text-2);
  }

  .filter span::after {
    content: ':';
  }

  .filter select {
    appearance: none;
    field-sizing: content;
    border: 0;
    background: transparent;
    font-weight: 500;
    font-size: 12px;
    color: var(--text);
    cursor: pointer;
    padding: 0;
  }

  .filter select:focus-visible {
    outline: none;
  }

  .filter:focus-within {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .filter.on {
    background: var(--surface-2);
    box-shadow: var(--press-sh);
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

  .legend .zero {
    opacity: 0.6;
  }

  .pills {
    display: none;
  }

  .cols {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 16px;
    padding: 0 24px 24px;
  }

  .col {
    display: flex;
    flex-direction: column;
    min-height: 0;
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
    font-size: 11px;
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

  /* Narrow desktop: columns keep a readable width and the board scrolls sideways. */
  @media (min-width: 900px) and (max-width: 1180px) {
    .cols {
      grid-template-columns: repeat(4, minmax(240px, 1fr));
      overflow-x: auto;
    }
  }

  /* Phone: one column at a time, chosen from pills; a floating New task button in thumb reach. */
  @media (max-width: 899px) {
    .bar {
      padding: 4px 16px 8px;
      min-height: 0;
      order: 2;
    }

    .filters {
      display: none;
    }

    .legend {
      font-size: 13px;
      gap: 6px 14px;
    }

    .legend .zero {
      display: none;
    }

    .pills {
      order: 1;
      display: flex;
      gap: 8px;
      padding: 4px 16px 8px;
      overflow-x: auto;
      scrollbar-width: none;
      flex: none;
    }

    .pill {
      flex: none;
      display: inline-flex;
      align-items: center;
      gap: 8px;
      height: 40px;
      padding: 0 14px;
      border-radius: 999px;
      border: 1px solid var(--border);
      background: var(--surface);
      color: var(--text-2);
      font-weight: 500;
      font-size: 14px;
    }

    .pill .mono {
      font-size: 12px;
    }

    .pill[aria-selected='true'] {
      background: linear-gradient(#2b2b2b, #0f0f0f);
      border-color: var(--text);
      color: #f5f5f2;
      box-shadow:
        inset 0 1px 0 rgba(255, 255, 255, 0.18),
        0 1px 2px rgba(21, 21, 21, 0.35);
    }

    :global(:root[data-theme='dark']) .pill[aria-selected='true'] {
      background: linear-gradient(#ffffff, #dcdcd7);
      color: #0b0b0c;
    }

    .view {
      height: auto;
    }

    .cols {
      order: 3;
      display: block;
      padding: 0 16px 96px;
    }

    .col {
      display: none;
      background: none;
      box-shadow: none;
      padding: 0;
    }

    .col[data-active='true'] {
      display: flex;
    }

    .ch {
      display: none;
    }

    .list {
      overflow: visible;
      padding: 0;
    }

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

  @media (max-width: 899px) and (prefers-color-scheme: dark) {
    :global(:root:not([data-theme='light'])) .pill[aria-selected='true'] {
      background: linear-gradient(#ffffff, #dcdcd7);
      color: #0b0b0c;
    }
  }
</style>
