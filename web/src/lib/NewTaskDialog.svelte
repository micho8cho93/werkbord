<script lang="ts">
  import { api } from './api';
  import { compact, executionAgents, resolveFor } from './execution';
  import ExecutionFields from './ExecutionFields.svelte';
  import { router } from './router.svelte';
  import Sheet from './Sheet.svelte';
  import { app } from './state.svelte';
  import type { ExecutionConfig, Project } from './types';

  // New work in one step: what it is, how it should run, and whether an agent starts on it now.
  // Everything the task can set is visible here; nothing waits behind an "Options" toggle.

  let { project }: { project: Project } = $props();

  let title = $state('');
  let description = $state('');
  let execution = $state<ExecutionConfig>({});
  let busy = $state(false);
  let error = $state('');

  const inherited = $derived(resolveFor({}, project.execution, app.globalExecution));
  const agentReady = $derived(executionAgents(app.agents, app.runners, project.id, execution.runner || inherited.runner).length > 0);

  function close() {
    app.newTaskOpen = false;
  }

  async function add(start: boolean) {
    const t = title.trim();
    if (!t || busy) return;
    busy = true;
    error = '';
    try {
      const task = await api.createTask(project.id, t, description.trim(), compact(execution));
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
      <span class="lab">What should an agent do?</span>
      <input class="input big" maxlength="200" placeholder="Fix the flaky auth test" bind:value={title} data-autofocus required />
    </label>
    <label class="field">
      <span class="lab">Details <span class="opt">optional</span></span>
      <textarea class="input" rows="3" placeholder="Context, constraints, where to look…" bind:value={description}></textarea>
    </label>
    <div class="exec">
      <ExecutionFields bind:value={execution} {inherited} idPrefix="new-task" dense />
    </div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>

  {#snippet footer()}
    <span class="tip"><kbd class="key">⌘</kbd><kbd class="key">↵</kbd> start</span>
    <button class="btn" type="button" onclick={close}>Cancel</button>
    <button class="btn" type="submit" form="new-task-form" disabled={busy || !title.trim()}>Add to Backlog</button>
    <button class="btn primary" type="button" disabled={busy || !title.trim() || !agentReady} onclick={() => add(true)} title={agentReady ? '' : 'No agent is available on an online runner'}>
      {busy ? 'Adding…' : 'Add and start agent'}
    </button>
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
