<script lang="ts">
  import { api } from '../api';
  import { timeAgo } from '../format';
  import { taskHref } from '../router.svelte';
  import type { ProjectScope } from '../scope.svelte';
  import { app } from '../state.svelte';
  import { TASK_STATE_LABELS, type Task } from '../types';
  let { scope }: { scope: ProjectScope } = $props();
  let search = $state('');
  let busy = $state('');
  const tasks = $derived(scope.archivedTasks.filter(t => `${t.title} ${t.description}`.toLowerCase().includes(search.toLowerCase())).sort((a,b) => b.archivedAt!.localeCompare(a.archivedAt!)));
  async function restore(task: Task) {
    if (busy) return;
    busy = task.id;
    try { scope.upsertTask(await api.archiveTask(task, false)); app.notify('Task restored to ' + TASK_STATE_LABELS[task.state] + '.'); }
    catch (e) { app.handleError(e); await scope.load().catch(e => app.handleError(e)); }
    finally { busy = ''; }
  }
</script>
<section class="archive" aria-label="Task archive">
  <div class="heading"><div><h2>Archived work</h2><p class="muted">Closed tasks and cleared Done cards. Their conversations, runs and Git evidence are kept.</p></div><label class="field">Search archive<input class="input" type="search" bind:value={search} placeholder="Title or task details" /></label></div>
  <ul class="tasks">
    {#each tasks as task (task.id)}
      <li><div><a href={taskHref(task.projectId, task.id)}>{task.title}</a><p class="muted">{TASK_STATE_LABELS[task.state]} · Archived {timeAgo(task.archivedAt!, app.now)}</p></div><button class="btn small" disabled={!!busy} onclick={() => restore(task)}>{busy === task.id ? 'Restoring…' : 'Restore'}</button></li>
    {:else}<li class="muted">{search ? 'No archived tasks match.' : 'No archived work yet. Clear Done or close a task to keep it here.'}</li>{/each}
  </ul>
</section>
<style>
  .archive { padding: 0 24px 24px; min-height: 0; display: flex; flex-direction: column; gap: 20px; }
  .heading { display: flex; align-items: end; justify-content: space-between; gap: 20px; flex-wrap: wrap; }
  h2 { font-size: 18px; } .field { display: grid; gap: 4px; min-width: min(280px, 100%); }
  .tasks { list-style: none; padding: 0; margin: 0; overflow-y: auto; scrollbar-gutter: stable; }
  li { display: flex; justify-content: space-between; gap: 16px; padding: 16px 0; border-bottom: 1px solid var(--border); }
  li div { min-width: 0; } a { font-weight: 500; overflow-wrap: anywhere; } p { font-size: 12px; }
</style>
