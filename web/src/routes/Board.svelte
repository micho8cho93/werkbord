<script lang="ts">
  import { ApiError, api } from '../lib/api';
  import { agentName, cardActivity, oneLine, runElapsed, runStatus } from '../lib/format';
  import { compact, hasOverrides, priorityLabel, resolveFor, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import { interactionShort } from '../lib/policy';
  import { kindLabel } from '../lib/questions';
  import RunBadge from '../lib/RunBadge.svelte';
  import { globalHref, taskHref } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import {
    TASK_STATES,
    TASK_STATE_LABELS,
    type ExecutionConfig,
    type Project,
    type Task,
    type TaskState,
  } from '../lib/types';

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  // On a phone one column is visible at a time; this is the one shown.
  let activeColumn = $state<TaskState>('backlog');
  let newTitle = $state('');
  /** What the new task overrides; empty means it inherits everything from the project and your defaults. */
  let newExecution = $state<ExecutionConfig>({});
  let showOptions = $state(false);
  /** What a task here gets if it sets nothing itself: the project's defaults over your global ones. */
  const inherited = $derived(resolveFor({}, project.execution, app.globalExecution));
  const optionsNote = $derived(hasOverrides(compact(newExecution)) ? summaryLine(resolveFor(compact(newExecution), project.execution, app.globalExecution), app.agents, app.agentOptions) || 'Customised' : '');
  let busy = $state(false);
  let notice = $state('');

  const columns = $derived(
    TASK_STATES.map((state) => ({
      state,
      tasks: scope.tasks.filter((t) => t.state === state).sort((a, b) => a.position - b.position),
    })),
  );

  /** Tasks whose agent needs the user: a question, a next message, or a decision it would not guess. */
  const waiting = $derived(scope.taskCount((r) => runStatus(r).needsInput));

  async function addTask(e: SubmitEvent) {
    e.preventDefault();
    const title = newTitle.trim();
    if (!title) return;
    busy = true;
    try {
      scope.upsertTask(await api.createTask(project.id, title, '', compact(newExecution)));
      newTitle = '';
      newExecution = {};
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
      scope.upsertTask(await api.moveTask(task, state));
      notice = '';
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        notice = 'This task changed on another device. The board has been refreshed.';
        await scope.load().catch((e) => app.handleError(e));
      } else {
        notice = err instanceof Error ? err.message : String(err);
      }
    }
  }
</script>

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
    <a href={globalHref('control')}>Open Control Center</a>
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
          <div class="add-row">
            <label class="visually-hidden" for="new-task">New task</label>
            <input id="new-task" class="input" placeholder="Add a task" maxlength="200" bind:value={newTitle} />
            <button class="btn primary" type="submit" disabled={busy || !newTitle.trim()}>Add</button>
          </div>
          <button type="button" class="options-toggle" aria-expanded={showOptions} onclick={() => (showOptions = !showOptions)}>
            {showOptions ? 'Hide options' : 'Options'}
            {#if !showOptions && optionsNote}<span class="chosen">· {optionsNote}</span>{/if}
          </button>
          {#if showOptions}
            <ExecutionFields bind:value={newExecution} {inherited} idPrefix="new-task" />
          {/if}
        </form>
      {/if}

      <ul class="cards">
        {#each col.tasks as task (task.id)}
          {@const run = scope.latestRun[task.id]}
          {@const status = run ? runStatus(run) : undefined}
          {@const ask = run ? scope.pendingFor(run.id) : []}
          {@const eff = resolveFor(task.execution, project.execution, app.globalExecution)}
          <li class="card task" data-tone={status?.tone} data-needs={status?.needsInput}>
            <a class="title" href={taskHref(project.id, task.id)}>{task.title}</a>
            {#if run && status}
              <div class="run">
                <div class="run-line">
                  <RunBadge {run} />
                  <span class="muted meta">{agentName(app.agents, run.agentId)} · {runElapsed(run, app.now)}</span>
                </div>
                {#if ask.length}
                  <p class="asking">
                    <strong>{ask.length > 1 ? `${ask.length} questions` : kindLabel(ask[0].kind)}:</strong>
                    {oneLine(ask[0].prompt, 120)}
                  </p>
                {:else if run.state === 'blocked'}
                  <p class="blocked">
                    <strong>Blocked:</strong>
                    {oneLine(cardActivity(run), 120)}
                  </p>
                {:else if cardActivity(run)}
                  <p class="activity" class:bad={run.state === 'failed'}>{cardActivity(run)}</p>
                {/if}
              </div>
            {/if}
            <div class="foot">
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
              {#if eff.priority !== 'normal'}
                <span class="policy" data-priority={eff.priority} title="Priority: {priorityLabel(eff.priority)}">{priorityLabel(eff.priority)} priority</span>
              {/if}
              {#if eff.interaction !== 'interactive'}
                <span class="policy" title="Interaction: {interactionShort({ interaction: eff.interaction })}">{interactionShort({ interaction: eff.interaction })}</span>
              {/if}
              {#if task.execution.agent || task.execution.model || task.execution.reasoning}
                <span class="policy" title="Set on this task">{summaryLine(eff, app.agents, app.agentOptions)}</span>
              {/if}
            </div>
          </li>
        {:else}
          <li class="none muted">{scope.loaded ? 'Nothing here.' : 'Loading…'}</li>
        {/each}
      </ul>
    </section>
  {/each}
</div>

<style>
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
    display: grid;
    gap: 8px;
    margin-bottom: 12px;
  }

  .add-row {
    display: flex;
    gap: 8px;
  }

  .options-toggle {
    justify-self: start;
    min-height: 32px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--accent);
    font-size: 0.85rem;
    font-weight: 550;
  }

  .options-toggle .chosen {
    color: var(--text-2);
    font-weight: 500;
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

  .task[data-tone='block'] {
    border-left: 3px solid var(--block);
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

  /* What the agent is asking, right on the card. */
  .asking {
    font-size: 0.84rem;
    line-height: 1.35;
    color: var(--text);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .asking strong {
    color: var(--warn);
  }

  /* Stopped rather than guess: what is in the way, right on the card. */
  .blocked {
    font-size: 0.84rem;
    line-height: 1.35;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .blocked strong {
    color: var(--block);
  }

  .waiting {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 12px;
    margin-bottom: 12px;
    font-size: 0.9rem;
  }

  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 10px;
  }

  .policy[data-priority='high'] {
    color: var(--danger);
  }

  .policy {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-2);
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--surface-2);
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
