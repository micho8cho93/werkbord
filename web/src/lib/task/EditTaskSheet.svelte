<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { ApiError, api } from '../api';
  import { agentWorkable } from '../task-presentation';
  import DependencyPicker from '../DependencyPicker.svelte';
  import { compact, type Resolved } from '../execution';
  import { editableOrchestration, emptyOrchestration, instantFromWall, wallTime } from '../scheduling';
  import TaskPlanFields from '../TaskPlanFields.svelte';
  import { modeOf } from '../labels';
  import ExecutionFields from '../ExecutionFields.svelte';
  import Sheet from '../Sheet.svelte';
  import type { ProjectScope } from '../scope.svelte';
  import { app } from '../state.svelte';
  import { openAgents } from '../agent-nav';
  import { runKey } from '../agent-sessions';
  import { router } from '../router.svelte';
  import { chainOf, pushed, type Origin } from '../origin';
  import { runStatus } from '../format';
  import { validatePlanDraft } from '../planning';
  import type { ExecutionConfig, Plan, PlanIssue, Task, WorkMode } from '../types';

  // The task's words and how its runs are carried out. A change applies to runs started after
  // it; a run already working keeps what it started with.

  let { task, scope, inherited, onclose, side = false, onschedule }: { task: Task; scope: ProjectScope; inherited: Resolved; onclose: () => void; side?: boolean; onschedule?: () => void } = $props();

  // A session opened from here leads back to this task, and from the task to wherever the person was (the Calendar, say).
  // Worked out when the button is pressed, not kept: the sheet's task is gone by the time it is closing.
  function routeBack(): Origin[] {
    const given = chainOf(router.query);
    const base: Origin[] = !given.length && router.view === 'calendar' ? [{ kind: 'calendar', projectId: task.projectId }] : given;
    return pushed({ kind: 'task', projectId: task.projectId, taskId: task.id }, base);
  }

  // Edits start from the task as it was when the sheet opened: a change elsewhere does not overwrite typing.
  const start = untrack(() => ({
    version: task.version,
    title: task.title,
    description: task.description,
    execution: { ...task.execution },
    labelIds: [...(task.labelIds ?? [])],
    workMode: modeOf(task),
    plan: { ...(task.plan ?? {}) },
    dependencies: [...(task.orchestration?.dependencies ?? [])],
    scheduledAt: task.orchestration?.scheduledAt,
    zone: task.orchestration?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone,
  }));
  const editKey = (t: string, d: string, e: ExecutionConfig, labels: string[], mode: WorkMode, p: Plan, deps: string[], at: string) =>
    JSON.stringify([t.trim(), d, compact(e), [...labels].sort(), mode, p.start || '', p.end || '', !!p.milestone, [...deps].sort(), at]);
  const initialScheduled = start.scheduledAt ? wallTime(start.scheduledAt, start.zone) : '';
  const initialKey = editKey(start.title, start.description, start.execution, start.labelIds, start.workMode, start.plan, start.dependencies, initialScheduled);
  let title = $state(start.title);
  let description = $state(start.description);
  let execution = $state<ExecutionConfig>(start.execution);
  let labelIds = $state<string[]>(start.labelIds);
  let workMode = $state<WorkMode>(start.workMode);
  let plan = $state<Plan>(start.plan);
  let dependencies = $state<string[]>(start.dependencies);
  let scheduled = $state(initialScheduled);
  const dirty = $derived(editKey(title, description, execution, labelIds, workMode, plan, dependencies, scheduled) !== initialKey);
  const project = $derived(app.project(task.projectId));
  // The task's latest run is a session like any other: the Calendar, Scheduling and the task page all open it in Agents by its key.
  const decision = $derived(scope.decisions.find(d => d.taskId === task.id));
  const latest = $derived(scope.latestRun[task.id]);
  const canAgent = $derived(agentWorkable({ workMode }, project));
  let busy = $state(false);
  let error = $state('');
  let serverIssues = $state<PlanIssue[]>([]);
  const draftTask = $derived({ ...task, title: title.trim(), description, plan, orchestration: { ...(task.orchestration ?? {}), enabled: task.orchestration?.enabled ?? false, dependencies, expectedPaths: task.orchestration?.expectedPaths ?? [] } });
  const draftIssues = $derived(validatePlanDraft(scope.tasks, draftTask));
  const issues = $derived([...draftIssues, ...serverIssues.filter((issue) => !draftIssues.some((draft) => draft.field === issue.field && draft.code === issue.code))]);
  const planIssues = $derived(issues.filter((issue) => issue.field.startsWith('plan.')));
  const dependencyIssue = $derived(issues.find((issue) => issue.field === 'dependencies')?.message ?? '');

  $effect(() => {
    void [plan.start, plan.end, plan.milestone, JSON.stringify(dependencies)];
    serverIssues = [];
  });

  async function focusIssue(issue: PlanIssue) {
    await tick();
    const id = issue.field === 'plan.start' ? 'edit-task-start' : issue.field === 'plan.end' ? 'edit-task-end' : `dependencies-${task.id}`;
    document.getElementById(id)?.focus({ preventScroll: false });
  }

  async function save() {
    if (!title.trim() || busy || !dirty) return;
    busy = true;
    error = '';
    serverIssues = [];
    if (draftIssues.length) {
      error = 'Fix the highlighted planning issues.';
      await focusIssue(draftIssues[0]);
      busy = false;
      return;
    }
    try {
      const edit: import('../api').TaskEdit = {
        title: title.trim(),
        description,
        execution: compact(execution),
        workMode,
        labelIds,
        plan: plan.start || plan.end ? plan : {},
      };
      // Dependencies are the scheduler's too, so they are written only when they were changed.
      if (JSON.stringify([...dependencies].sort()) !== JSON.stringify([...start.dependencies].sort())) {
        edit.orchestration = { ...editableOrchestration(task.orchestration ?? emptyOrchestration()), dependencies };
      }
      if (side && start.scheduledAt && scheduled !== wallTime(start.scheduledAt, start.zone)) {
        edit.orchestration = { ...editableOrchestration(task.orchestration ?? emptyOrchestration()), ...edit.orchestration,
          scheduledAt: instantFromWall(scheduled, start.zone), timezone: start.zone };
      }
      const t = await api.editTask({ ...task, version: start.version }, edit);
      scope.upsertTask(t);
      onclose();
    } catch (err) {
      serverIssues = err instanceof ApiError ? (err.issues ?? []) : [];
      error = serverIssues.length ? 'Fix the highlighted planning issues.' : err instanceof Error ? err.message : String(err);
      if (serverIssues.length) await focusIssue(serverIssues[0]);
      if (error.includes('modified') || error.includes('version')) await scope.load().catch((e) => app.handleError(e));
    } finally {
      busy = false;
    }
  }
</script>

<Sheet title="Edit task" {onclose} {side} width="38rem">
  <form
    id="edit-task-form"
    class="form"
    onsubmit={(e) => {
      e.preventDefault();
      void save();
    }}
  >
    {#if decision && canAgent}<p class="muted note">{decision.reason}</p>{/if}
    {#if onschedule && canAgent}<button class="btn" type="button" disabled={busy || dirty} onclick={onschedule}>Schedule and dependencies</button>{#if dirty}<p class="muted note">Save or cancel your changes before opening schedule controls.</p>{/if}{/if}
    {#if latest}
      <p class="session-link" data-testid="task-session">Latest session: {runStatus(latest).label}.
        <button class="btn small" type="button" onclick={() => { const key = runKey(latest.id); const from = routeBack(); onclose(); openAgents(key, from); }}>Open session</button></p>
    {/if}
    <label class="field">
      <span>Title</span>
      <input class="input" maxlength="200" bind:value={title} required data-autofocus />
    </label>
    <label class="field">
      <span>Details</span>
      <textarea class="input" rows="4" bind:value={description}></textarea>
    </label>
    <TaskPlanFields bind:plan repository={project?.kind !== 'work'} idPrefix="edit-task" issues={planIssues} />
    {#if side && canAgent && start.scheduledAt}
      <label class="field"><span>Agent start time ({start.zone})</span><input class="input" type="datetime-local" bind:value={scheduled} disabled={!!task.orchestration?.runId} required /></label>
      <p class="muted note">{task.orchestration?.runId ? 'This schedule has already been dispatched. Plan a new run from the task’s schedule controls.' : 'Changing this time reschedules the agent start. Planned dates stay separate.'}</p>
    {/if}
    <DependencyPicker tasks={scope.tasks} taskId={task.id} bind:selected={dependencies} error={dependencyIssue} />
    {#if canAgent}
      <div class="exec">
        <ExecutionFields bind:value={execution} {inherited} idPrefix="edit-task" dense />
      </div>
      <p class="muted note">A change applies to runs started after it. A run that is already working keeps its own.</p>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={onclose}>Cancel</button>
    <button class="btn primary" type="submit" form="edit-task-form" disabled={busy || !title.trim() || !dirty}>{busy ? 'Saving…' : side ? 'Save changes' : 'Save'}</button>
  {/snippet}
</Sheet>

<style>
  .form {
    display: grid;
    gap: 14px;
  }

  .field {
    display: grid;
    gap: 6px;
    font-weight: 500;
  }

  .exec {
    padding: 12px;
    border-radius: var(--radius-tray);
    background: var(--bg);
    border: 1px solid var(--border);
  }

  .note {
    font-size: 12px;
  }
.session-link { margin: 0; font-size: 13px; color: var(--text-2); display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
</style>
