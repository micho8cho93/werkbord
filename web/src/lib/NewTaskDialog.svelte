<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError, api } from './api';
  import { BUSY_RUNNERS, agentLabel, compact, executionAgents, onlyBusy, resolveFor, sourceLabel } from './execution';
  import { agentWorkable } from './board';
  import { INTERACTION_OPTIONS, PERMISSIONS_NOTE } from './policy';
  import { globalHref } from './router.svelte';
  import ExecutionFields from './ExecutionFields.svelte';
  import { router } from './router.svelte';
  import Sheet from './Sheet.svelte';
  import TaskPlanFields from './TaskPlanFields.svelte';
  import { app } from './state.svelte';
  import type { ExecutionConfig, Plan, Project, Task, WorkMode } from './types';

  // Essentials first; saved work survives a failed launch.

  let { project }: { project: Project } = $props();

  let title = $state('');
  let description = $state('');
  let execution = $state<ExecutionConfig>({});
  let labelIds = $state<string[]>([]);
  // A work project has no repository for an agent, so its tasks start out as work for a person.
  let workMode = $state<WorkMode>(untrack(() => (project.kind === 'work' ? 'human' : 'agent')));
  let plan = $state<Plan>({});
  let busy = $state(false);
  let error = $state('');
  let saved = $state<Task | null>(null);
  let phase = $state('');
  let uncertainSave = $state(false);

  const inherited = $derived(resolveFor({}, project.execution, app.globalExecution));
  const isWork = $derived(project.kind === 'work');
  const canAgent = $derived(agentWorkable({ workMode }, project));
  const effective = $derived(resolveFor(execution, project.execution, app.globalExecution));
  const agents = $derived(executionAgents(app.agents, app.runners, project.id, effective.runner));
  const agentReady = $derived(canAgent && app.connection === 'live' && (effective.agent ? agents.some(a => a.id === effective.agent) : agents.length > 0));
  const busyOnly = $derived(!agentReady && app.connection === 'live' && onlyBusy(app.agents, app.runners, project.id, effective.runner, effective.agent ?? ''));
  const interaction = $derived(INTERACTION_OPTIONS.find(o => o.value === effective.interaction)!);
  const runnerName = $derived(app.runners.find(r => r.id === effective.runner)?.name ?? (effective.runner && effective.runner !== 'automatic' ? 'Unavailable runner' : 'Automatic runner'));

  function close() {
    app.newTaskOpen = false;
  }

  async function add(start: boolean) {
    const t = title.trim();
    if (!t || busy || uncertainSave || (start && !saved && !agentReady)) return;
    busy = true;
    error = '';
    try {
      phase = saved ? 'Requesting agent…' : 'Saving task…';
      const task = saved ?? await api.createTask(project.id, t, description.trim(), isWork ? undefined : compact(execution), {
        workMode,
        labelIds,
        plan: plan.start || plan.end ? { ...plan, ...(plan.milestone ? {} : { end: plan.end || undefined }) } : undefined,
      });
      saved = task;
      const scope = app.scope(project.id);
      scope?.upsertTask(task);
      if (start) {
        phase = 'Requesting agent…';
        // A lost launch response can have started or even finished a run. Recover it before retrying.
        const runs = await api.listTaskRuns(project.id, task.id);
        const existing = runs.at(-1); // task history is oldest first
        if (!existing && !agentReady) throw new Error('The selected agent or runner is unavailable. Retry when it is ready.');
        const run = existing ?? await api.startRun(project.id, task.id);
        scope?.upsertRun(run);
        router.go({ view: 'task', projectId: project.id, taskId: task.id });
      } else {
        app.notify(`“${t}” saved.`, 4000);
      }
      close();
    } catch (err) {
      uncertainSave = !saved && err instanceof ApiError && (err.code === 'offline' || err.status >= 500);
      error = uncertainSave ? 'Could not confirm whether the task was saved. Check the Board before adding it again.' : `${saved ? 'Task saved. Agent could not start: ' : ''}${err instanceof Error ? err.message : String(err)}`;
    } finally {
      busy = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void add(canAgent);
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<Sheet title="New task in {project.name}" onclose={close} width="40rem">
  <form
    id="new-task-form"
    class="form"
    onsubmit={(e) => {
      e.preventDefault();
      void add(false);
    }}
  >
    <label class="field">
      <span class="lab">{isWork ? 'What needs doing?' : 'What should an agent do?'}</span>
      <input class="input big" disabled={!!saved || uncertainSave} maxlength="200" placeholder={isWork ? 'Book the venue' : 'Fix the flaky auth test'} bind:value={title} data-autofocus required />
    </label>
    <label class="field">
      <span class="lab">Details <span class="opt">optional</span></span>
      <textarea class="input" disabled={!!saved || uncertainSave} rows="2" placeholder={isWork ? 'Context, links, who to ask…' : 'Context, constraints, where to look…'} bind:value={description}></textarea>
    </label>
    {#if canAgent}
      <section class="exec" aria-label="Execution summary">
        <strong>{effective.agent ? agentLabel(app.agents, effective.agent) : 'Automatic agent'} on {runnerName} · {interaction.label}</strong>
        <p class="hint">Permission requests still come to you.</p>
        {#if busyOnly}<p class="hint" role="status">{BUSY_RUNNERS} Add it to Backlog now and start it from its card.</p>
        {:else if !agentReady}<p class="hint">{app.connection !== 'live' ? 'Reconnect to confirm availability before starting.' : 'The selected agent or runner is unavailable.'} You can save this task in Backlog. <a href={globalHref('settings')}>Check Settings</a>.</p>{/if}
        <details><summary>Change execution settings</summary><p class="hint">{interaction.description} {PERMISSIONS_NOTE} From {sourceLabel(effective.sources.interaction)}.</p><ExecutionFields bind:value={execution} {inherited} idPrefix="new-task" disabled={!!saved || uncertainSave} columns /></details>
      </section>
    {/if}
    <details><summary>Labels, ownership and planning</summary><fieldset disabled={!!saved || uncertainSave}><TaskPlanFields bind:labelIds bind:workMode bind:plan repository={!isWork} idPrefix="new-task" /></fieldset></details>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if saved}<button class="btn" type="button" onclick={() => { if (saved) router.go({ view: 'task', projectId: project.id, taskId: saved.id }); close(); }}>View saved task</button>{/if}
  </form>

  {#snippet footer()}
    {#if canAgent}<span class="tip"><kbd class="key">⌘</kbd><kbd class="key">↵</kbd> start</span>{/if}
    <button class="btn" type="button" onclick={close}>{saved || uncertainSave ? 'Close' : 'Cancel'}</button>
    <button class="btn" class:primary={!canAgent} type="submit" form="new-task-form" disabled={busy || !title.trim() || uncertainSave}>{saved ? 'Keep saved task' : 'Add to Backlog'}</button>
    {#if canAgent}
      <button class="btn primary" type="button" disabled={busy || !title.trim() || (!saved && !agentReady) || uncertainSave} onclick={() => add(true)} title={agentReady ? '' : busyOnly ? 'Every runner that can take this task is busy' : 'No agent is available on an online runner'}>
        {busy ? phase : saved ? 'Retry start' : 'Add and start agent'}
      </button>
    {/if}
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
  }

  .lab {
    font-size: 13px;
    font-weight: 500;
  }

  .opt {
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--text-2);
    margin-left: 4px;
  }

  .big {
    min-height: 40px;
    font-size: 15px;
  }

  .exec {
    padding: 12px;
    border-radius: var(--radius-tray);
    background: var(--bg);
    border: 1px solid var(--border);
  }

  fieldset { border: 0; margin: 0; padding: 0; min-width: 0; display: grid; gap: 14px; }
  summary { cursor: pointer; min-height: 44px; display: flex; align-items: center; font-size: 13px; font-weight: 500; }
  .exec p { margin-top: 6px; font-size: 13px; }
  .exec .hint { font-size: 12px; color: var(--text-2); }

  .tip {
    margin-right: auto;
    align-self: center;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  @media (max-width: 719px) {
    fieldset { border: 0; margin: 0; padding: 0; min-width: 0; display: grid; gap: 14px; }
  summary { cursor: pointer; min-height: 44px; display: flex; align-items: center; font-size: 13px; font-weight: 500; }
  .exec p { margin-top: 6px; font-size: 13px; }
  .exec .hint { font-size: 12px; color: var(--text-2); }

  .tip {
      display: none;
    }
  }
</style>
