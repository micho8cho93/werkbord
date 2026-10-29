<script lang="ts">
  import { cardStatus } from '../board';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import { spanOf, spanTitle } from '../planning-dates';
  import { TASK_STATE_LABELS, type Task } from '../types';
  import { formatDateTime } from '../clock.svelte';

  // One task as a row: its status as the board words it (running, needs you, blocked, ready for review), where it is in its plan, and
  // the whole row opens the task, where everything the person may do about it is.
  let { task, note = '' }: { task: Task; note?: string } = $props();

  const scope = $derived(app.scopes.get(task.projectId));
  const run = $derived(scope?.latestRun[task.id]);
  const project = $derived(app.project(task.projectId));
  const status = $derived(
    cardStatus(task, run, {
      now: app.now,
      questions: run ? (scope?.pendingFor(run.id) ?? []) : [],
      decision: scope?.decisions.find((d) => d.taskId === task.id),
      waitingFor: [],
    }),
  );
  const span = $derived(spanOf(task));
  const when = $derived(
    span
      ? `${span.milestone ? 'Milestone' : 'Planned'} ${spanTitle(span, new Date().getFullYear())}`
      : task.orchestration?.enabled && task.orchestration.scheduledAt
        ? `Agent starts ${formatDateTime(task.orchestration.scheduledAt)}`
        : '',
  );
</script>

<a class="row card" href={taskHref(task.projectId, task.id)} data-testid="work-row" data-tone={status.tone}>
  <span class="dot" data-tone={status.tone} aria-hidden="true"></span>
  <span class="what">
    <span class="title">{task.title}</span>
    <span class="meta mm">{project?.name ?? ''} · {TASK_STATE_LABELS[task.state]}{when ? ` · ${when}` : ''}</span>
    {#if run || status.text !== 'Not started'}<span class="status mm" data-tone={status.tone}>{status.text}</span>{/if}
    {#if status.detail}<span class="well">{status.detail}</span>{/if}
    {#if note}<span class="note mm">{note}</span>{/if}
  </span>
</a>

<style>
  .row {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    min-height: 56px;
    padding: 12px 14px;
    color: var(--text);
    text-decoration: none;
    -webkit-tap-highlight-color: transparent;
  }

  .row:active {
    box-shadow: var(--press-sh);
    background: var(--surface-2);
  }

  .row[data-tone='ask'] {
    border-color: color-mix(in srgb, var(--warn) 55%, var(--border));
  }

  .dot {
    margin-top: 6px;
    flex: none;
  }

  .what {
    display: grid;
    gap: 4px;
    min-width: 0;
  }

  .title {
    font-size: 14px;
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .meta,
  .status,
  .note {
    overflow-wrap: anywhere;
  }

  .status[data-tone='ask'] {
    color: var(--warn-text);
  }

  .status[data-tone='bad'] {
    color: var(--danger-text);
  }

  .status[data-tone='ok'] {
    color: var(--ok-text);
  }
</style>
