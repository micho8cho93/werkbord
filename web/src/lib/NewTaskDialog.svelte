<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from './api';
  import { compact, executionAgents, resolveFor } from './execution';
  import { agentWorkable } from './board';
  import ExecutionFields from './ExecutionFields.svelte';
  import { router } from './router.svelte';
  import Sheet from './Sheet.svelte';
  import TaskPlanFields from './TaskPlanFields.svelte';
  import { app } from './state.svelte';
  import type { ExecutionConfig, Plan, Project, WorkMode } from './types';

  // New work in one step: what it is, how it should run, and whether an agent starts on it now.
  // Everything the task can set is visible here; nothing waits behind an "Options" toggle.

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

  const inherited = $derived(resolveFor({}, project.execution, app.globalExecution));
  const isWork = $derived(project.kind === 'work');
  const canAgent = $derived(agentWorkable({ workMode }, project));
  const agentReady = $derived(canAgent && executionAgents(app.agents, app.runners, project.id, execution.runner || inherited.runner).length > 0);

  function close() {
    app.newTaskOpen = false;
  }

  async function add(start: boolean) {
    const t = title.trim();
    if (!t || busy) return;
    busy = true;
    error = '';
    try {
      const task = await api.createTask(project.id, t, description.trim(), isWork ? undefined : compact(execution), {
        workMode,
        labelIds,
        plan: plan.start || plan.end ? { ...plan, ...(plan.milestone ? {} : { end: plan.end || undefined }) } : undefined,
      });
      const scope = app.scope(project.id);
      scope?.upsertTask(task);
      if (start) {
        const run = await api.startRun(project.id, task.id);
        scope?.upsertRun(run);
        router.go({ view: 'task', projectId: project.id, taskId: task.id });
      } else {
        app.notify(`“${t}” is in Backlog.`, 4000);
      }
      close();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void add(agentReady);
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
      <input class="input big" maxlength="200" placeholder={isWork ? 'Book the venue' : 'Fix the flaky auth test'} bind:value={title} data-autofocus required />
    </label>
    <label class="field">
      <span class="lab">Details <span class="opt">optional</span></span>
      <textarea class="input" rows="3" placeholder={isWork ? 'Context, links, who to ask…' : 'Context, constraints, where to look…'} bind:value={description}></textarea>
    </label>
    <TaskPlanFields bind:labelIds bind:workMode bind:plan repository={!isWork} idPrefix="new-task" />
    {#if canAgent}
      <div class="exec">
        <ExecutionFields bind:value={execution} {inherited} idPrefix="new-task" dense />
      </div>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>

  {#snippet footer()}
    {#if canAgent}<span class="tip"><kbd class="key">⌘</kbd><kbd class="key">↵</kbd> start</span>{/if}
    <button class="btn" type="button" onclick={close}>Cancel</button>
    <button class="btn" class:primary={!canAgent} type="submit" form="new-task-form" disabled={busy || !title.trim()}>Add to Backlog</button>
    {#if canAgent}
      <button class="btn primary" type="button" disabled={busy || !title.trim() || !agentReady} onclick={() => add(true)} title={agentReady ? '' : 'No agent is available on an online runner'}>
        {busy ? 'Adding…' : 'Add and start agent'}
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
    .tip {
      display: none;
    }
  }
</style>
