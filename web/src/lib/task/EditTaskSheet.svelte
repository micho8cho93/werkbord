<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../api';
  import { compact, type Resolved } from '../execution';
  import ExecutionFields from '../ExecutionFields.svelte';
  import Sheet from '../Sheet.svelte';
  import type { ProjectScope } from '../scope.svelte';
  import { app } from '../state.svelte';
  import type { ExecutionConfig, Task } from '../types';

  // The task's words and how its runs are carried out. A change applies to runs started after
  // it; a run already working keeps what it started with.

  let { task, scope, inherited, onclose }: { task: Task; scope: ProjectScope; inherited: Resolved; onclose: () => void } = $props();

  // Edits start from the task as it was when the sheet opened: a change elsewhere does not overwrite typing.
  const start = untrack(() => ({ version: task.version, title: task.title, description: task.description, execution: { ...task.execution } }));
  let title = $state(start.title);
  let description = $state(start.description);
  let execution = $state<ExecutionConfig>(start.execution);
  let busy = $state(false);
  let error = $state('');

  async function save() {
    if (!title.trim() || busy) return;
    busy = true;
    error = '';
    try {
      const t = await api.editTask({ ...task, version: start.version }, { title: title.trim(), description, execution: compact(execution) });
      scope.upsertTask(t);
      onclose();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      if (error.includes('modified') || error.includes('version')) await scope.load().catch((e) => app.handleError(e));
    } finally {
      busy = false;
    }
  }
</script>

<Sheet title="Edit task" {onclose} width="38rem">
  <form
    id="edit-task-form"
    class="form"
    onsubmit={(e) => {
      e.preventDefault();
      void save();
    }}
  >
    <label class="field">
      <span>Title</span>
      <input class="input" maxlength="200" bind:value={title} required data-autofocus />
    </label>
    <label class="field">
      <span>Details</span>
      <textarea class="input" rows="4" bind:value={description}></textarea>
    </label>
    <div class="exec">
      <ExecutionFields bind:value={execution} {inherited} idPrefix="edit-task" dense />
    </div>
    <p class="muted note">A change applies to runs started after it. A run that is already working keeps its own.</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={onclose}>Cancel</button>
    <button class="btn primary" type="submit" form="edit-task-form" disabled={busy || !title.trim()}>{busy ? 'Saving…' : 'Save'}</button>
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
</style>
