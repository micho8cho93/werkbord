<script lang="ts">
  import { formatDateTime } from '../clock.svelte';
  import { resolveFor } from '../execution';
  import Sheet from '../Sheet.svelte';
  import { schedulingLabels } from '../scheduling';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import EditTaskSheet from '../task/EditTaskSheet.svelte';
  import { spanTitle } from '../timeline';
  import { TASK_STATE_LABELS, type Task } from '../types';
  import { calendarItems, type CalItem } from './calendar';
  import MonthView from './MonthView.svelte';
  import { cal, type CalMode } from './calstate.svelte';
  import Segmented from '../Segmented.svelte';
  import TimelineView from './TimelineView.svelte';
  import WeekView from './WeekView.svelte';
  import { projectsLoaded, tasksOf, warmProjects } from './work.svelte';
  import { modeOf } from '../labels';

  // The phone's Calendar: the planned work and the agent starts of every project (or one), as a month, a week or a timeline. It reads
  // the tasks each project already holds and changes nothing itself; opening an item offers the task, and the same editor the board uses.
  const modes = [
    { id: 'month', label: 'Month' },
    { id: 'week', label: 'Week' },
    { id: 'timeline', label: 'Timeline' },
  ] as const;

  $effect(() => warmProjects());

  const tasks = $derived(tasksOf(cal.project));
  const items = $derived(calendarItems(tasks, (id) => app.project(id)?.name ?? id));
  const ready = $derived(projectsLoaded());
  let shown = $state<CalItem | null>(null);
  let editing = $state<Task | null>(null);

  const taskOf = (item: CalItem) => app.scopes.get(item.projectId)?.tasks.find((t) => t.id === item.taskId) ?? null;
  const decision = $derived(shown ? app.scopes.get(shown.projectId)?.decisions.find((d) => d.taskId === shown!.taskId) : undefined);
  const editable = $derived(shown ? taskOf(shown) : null);
  function openFromTimeline(taskId: string, kind: 'planned' | 'scheduled') {
    const found = items.find((i) => i.taskId === taskId && i.kind === kind) ?? items.find((i) => i.taskId === taskId);
    if (found) shown = found;
  }
  function edit() {
    const t = editable;
    shown = null;
    editing = t;
  }
  const below = $derived(editing ? resolveFor({}, app.project(editing.projectId)?.execution, app.globalExecution) : resolveFor({}, undefined, app.globalExecution));
  const year = new Date().getFullYear();
</script>

<div class="schedule">
  <Segmented options={modes} value={cal.mode} onchange={(m: CalMode) => (cal.mode = m)} label="Calendar view" />

  {#if app.projects.length > 1}
    <label class="pick" class:on={cal.project}>
      <span>Project</span>
      <select bind:value={cal.project} aria-label="Show one project">
        <option value="">all</option>
        {#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
      </select>
    </label>
  {/if}

  {#if !ready}
    <p class="note" role="status">Loading the work of every project…</p>
  {:else if items.length === 0 && cal.mode !== 'timeline'}
    <p class="note">Nothing is planned or scheduled{cal.project ? ' in this project' : ''} yet. Plan dates on a task to see them here.</p>
  {/if}

  {#if cal.mode === 'month'}
    <MonthView {items} onopen={(i) => (shown = i)} />
  {:else if cal.mode === 'week'}
    <WeekView {items} onopen={(i) => (shown = i)} />
  {:else}
    <TimelineView {tasks} onopen={openFromTimeline} />
  {/if}
</div>

{#if shown}
  <Sheet title={shown.title} onclose={() => (shown = null)}>
    <dl class="facts">
      <div><dt>Project</dt><dd>{shown.projectName}</dd></div>
      <div><dt>Status</dt><dd>{TASK_STATE_LABELS[shown.state]}</dd></div>
      {#if shown.kind === 'planned'}
        <div>
          <dt>{shown.milestone ? 'Milestone' : 'Planned'}</dt>
          <dd>{spanTitle({ start: shown.start, end: shown.end, milestone: shown.milestone }, year)}</dd>
        </div>
      {:else if shown.at}
        <div><dt>An agent starts</dt><dd>{formatDateTime(shown.at)}</dd></div>
        {#if decision}<div><dt>Now</dt><dd>{schedulingLabels[decision.state]}. {decision.reason}</dd></div>{/if}
      {/if}
    </dl>
    <p class="hint mm">
      {shown.kind === 'planned'
        ? 'Planned dates are when the work is meant to happen. They never start an agent.'
        : 'This is when an agent is set to start. It does not change the planned dates.'}
    </p>
    {#snippet footer()}
      <a class="btn open" href={taskHref(shown!.projectId, shown!.taskId)} onclick={() => (shown = null)}>Open task</a>
      {#if editable && modeOf(editable) !== undefined}<button class="btn primary" type="button" onclick={edit}>Edit dates</button>{/if}
    {/snippet}
  </Sheet>
{/if}
{#if editing}<EditTaskSheet task={editing} scope={app.scopes.get(editing.projectId)!} inherited={below} onclose={() => (editing = null)} />{/if}

<style>
  .schedule {
    display: grid;
    gap: 12px;
    padding: 4px 16px 24px;
  }

  .note {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }

  .pick {
    justify-self: start;
    height: 40px;
  }

  .facts {
    display: grid;
    gap: 10px;
    margin: 0 0 12px;
  }

  .facts div {
    display: grid;
    gap: 2px;
  }

  dt {
    font-family: var(--mono);
    font-size: 11.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  dd {
    margin: 0;
    font-size: 14px;
  }

  .hint {
    margin: 0;
  }
</style>
