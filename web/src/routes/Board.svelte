<script lang="ts">
  import { ApiError, api } from '../lib/api';
  import { agentName, cardActivity, runElapsed, runStatus } from '../lib/format';
  import RunBadge from '../lib/RunBadge.svelte';
  import { taskHref } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import { TASK_STATES, TASK_STATE_LABELS, type Task, type TaskState } from '../lib/types';

  // On a phone one column is visible at a time; this is the one shown.
  let activeColumn = $state<TaskState>('backlog');
  let newTitle = $state('');
  let busy = $state(false);
  let notice = $state('');

  const columns = $derived(
    TASK_STATES.map((state) => ({
      state,
      tasks: app.tasks.filter((t) => t.state === state).sort((a, b) => a.position - b.position),
    })),
  );

  /** Tasks whose agent is waiting on the user, for the line under the column switcher. */
  const waiting = $derived(app.tasks.filter((t) => app.latestRun[t.id]?.state === 'waiting_for_user').length);

  async function addTask(e: SubmitEvent) {
    e.preventDefault();
    const title = newTitle.trim();
    if (!title || !app.selectedProjectId) return;
    busy = true;
    try {
      app.upsertTask(await api.createTask(app.selectedProjectId, title));
      newTitle = '';
      notice = '';
    } catch (err) {
      notice = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function move(task: Task, state: TaskState) {
    if (state === task.state) return;
    try {
      app.upsertTask(await api.moveTask(task, state));
      notice = '';
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        notice = 'This task changed on another device. The board has been refreshed.';
        await app.refresh();
      } else {
        notice = err instanceof Error ? err.message : String(err);
      }
    }
  }
</script>

{#if app.projects.length === 0}
  <div class="card empty">
    <p>No projects yet.</p>
    <p>Register a local Git repository in <a href="#/git">Git</a> to get a board.</p>
  </div>
{:else}
  <div class="toolbar">
    <label class="visually-hidden" for="project">Project</label>
    <select
      id="project"
      class="select project"
      value={app.selectedProjectId}
      onchange={(e) => app.selectProject(e.currentTarget.value)}
    >
      {#each app.projects as p (p.id)}
        <option value={p.id}>{p.name}</option>
      {/each}
    </select>
  </div>

  <div class="segments" role="tablist" aria-label="Columns">
    {#each columns as col (col.state)}
      <button
        role="tab"
        aria-selected={activeColumn === col.state}
        aria-controls="col-{col.state}"
        onclick={() => (activeColumn = col.state)}
      >
        {TASK_STATE_LABELS[col.state]}
        <span class="count">{col.tasks.length}</span>
      </button>
    {/each}
  </div>

  {#if waiting > 0}
    <p class="waiting" role="status">
      <span class="badge" data-tone="ask"><span class="dot" aria-hidden="true"></span>{waiting} {waiting === 1 ? 'task needs' : 'tasks need'} you</span>
      <a href="#/control">Open Control Center</a>
    </p>
  {/if}

  {#if notice}
    <p class="error notice" role="status">{notice}</p>
  {/if}

  <div class="columns">
    {#each columns as col (col.state)}
      <section id="col-{col.state}" class="column" data-active={activeColumn === col.state} aria-label={TASK_STATE_LABELS[col.state]}>
        <header class="column-head">
          <h2>{TASK_STATE_LABELS[col.state]}</h2>
          <span class="count">{col.tasks.length}</span>
        </header>

        {#if col.state === 'backlog'}
          <form class="add" onsubmit={addTask}>
            <label class="visually-hidden" for="new-task">New task</label>
            <input id="new-task" class="input" placeholder="Add a task" maxlength="200" bind:value={newTitle} />
            <button class="btn primary" type="submit" disabled={busy || !newTitle.trim()}>Add</button>
          </form>
        {/if}

        <ul class="cards">
          {#each col.tasks as task (task.id)}
            {@const run = app.latestRun[task.id]}
            {@const status = run ? runStatus(run) : undefined}
            <li class="card task" data-tone={status?.tone} data-needs={status?.needsInput}>
              <a class="title" href={taskHref(task.id)}>{task.title}</a>
              {#if run && status}
                <div class="run">
                  <div class="run-line">
                    <RunBadge {run} />
                    <span class="muted meta">{agentName(app.agents, run.agentId)} · {runElapsed(run, app.now)}</span>
                  </div>
                  {#if cardActivity(run)}
                    <p class="activity" class:bad={run.state === 'failed'}>{cardActivity(run)}</p>
                  {/if}
                </div>
              {/if}
              <label class="move">
                <span class="visually-hidden">Move “{task.title}” to</span>
                <select
                  class="select"
                  value={task.state}
                  onchange={(e) => move(task, e.currentTarget.value as TaskState)}
                >
                  {#each TASK_STATES as s (s)}
                    <option value={s}>{TASK_STATE_LABELS[s]}</option>
                  {/each}
                </select>
              </label>
            </li>
          {:else}
            <li class="none muted">Nothing here.</li>
          {/each}
        </ul>
      </section>
    {/each}
  </div>
{/if}

<style>
  .toolbar {
    margin-bottom: 12px;
  }

  .project {
    max-width: 320px;
    font-weight: 600;
  }

  .segments {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 4px;
    padding: 4px;
    margin-bottom: 12px;
    background: var(--surface-2);
    border-radius: var(--radius);
  }

  .segments button {
    min-height: 38px;
    padding: 0 4px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-2);
    font-size: 0.85rem;
    font-weight: 600;
    white-space: nowrap;
  }

  .segments button[aria-selected='true'] {
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow);
  }

  .count {
    margin-left: 4px;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-2);
  }

  .notice {
    margin-bottom: 12px;
  }

  .columns {
    display: block;
  }

  .column {
    display: none;
  }

  .column[data-active='true'] {
    display: block;
  }

  .column-head {
    display: none;
  }

  .add {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
  }

  .cards {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .task {
    display: grid;
    gap: 10px;
    padding: 12px;
  }

  .title {
    font-weight: 550;
    overflow-wrap: anywhere;
    color: inherit;
    text-decoration: none;
  }

  /* The whole card opens the task; the move control stays tappable above it. */
  .task {
    position: relative;
  }

  .title::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: var(--radius);
  }

  .task:hover {
    border-color: color-mix(in srgb, var(--accent) 35%, var(--border));
  }

  .task[data-needs='true'] {
    border-left: 3px solid var(--warn);
  }

  .task[data-tone='work'],
  .task[data-tone='idle'] {
    border-left: 3px solid var(--accent);
  }

  .task[data-needs='true'][data-tone='ask'] {
    border-left-color: var(--warn);
  }

  .task[data-tone='bad'] {
    border-left: 3px solid var(--danger);
  }

  .run {
    display: grid;
    gap: 3px;
  }

  .run-line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 10px;
  }

  .meta {
    font-size: 0.8rem;
  }

  .activity {
    font-size: 0.82rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .activity.bad {
    color: var(--danger);
  }

  .waiting {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 12px;
    margin-bottom: 12px;
    font-size: 0.9rem;
  }

  .move {
    position: relative;
    z-index: 1;
    justify-self: start;
  }

  .move .select {
    min-height: 34px;
    width: auto;
    font-size: 0.85rem;
    color: var(--text-2);
  }

  .none {
    padding: 16px 4px;
    font-size: 0.9rem;
  }

  /* Wide screens: all four columns side by side. */
  @media (min-width: 900px) {
    .segments {
      display: none;
    }

    .columns {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 16px;
      align-items: start;
    }

    .column,
    .column[data-active] {
      display: block;
      padding: 12px;
      background: var(--surface-2);
      border-radius: var(--radius);
      min-height: 200px;
    }

    .column-head {
      display: flex;
      align-items: baseline;
      gap: 6px;
      margin-bottom: 10px;
    }
  }
</style>
