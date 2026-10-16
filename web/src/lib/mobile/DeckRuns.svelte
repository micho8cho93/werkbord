<script lang="ts">
  import { app } from '../state.svelte';
  import { taskHref } from '../location';
  import SessionRow from './SessionRow.svelte';
  import type { DeckExecution } from './layout';
  let { history }: { history: DeckExecution[] } = $props();
  $effect(() => { for (const h of history) app.warm(h.projectId); });
</script>
{#if history.length}
  <section aria-label="Workflow executions" data-workspace="individual">
    <h2>Workflow executions</h2>
    {#if app.connection !== 'live'}<p class="muted" role="status">Live updates are disconnected. These are the last reported states.</p>{/if}
    <ul>
      {#each history.slice(0, 8) as h (h.requestId)}
        {@const scope = app.scope(h.projectId)}
        {@const task = scope?.tasks.find(t => t.id === h.taskId)}
        {@const run = scope?.latestRun[h.taskId]}
        {#if run && task}
          <SessionRow x={{ run, taskTitle: task.title, projectName: app.project(h.projectId)?.name ?? 'Repository' }} />
        {:else}
          <li><a class="btn" href={taskHref(h.projectId, h.taskId)}>{task?.title ?? 'Workflow task'} · {app.project(h.projectId)?.name ?? 'Repository'}</a><p class="muted">{!app.project(h.projectId) ? 'This repository is no longer available.' : scope?.error ? 'Execution could not be loaded. Open the task to retry.' : scope?.loaded ? 'No run reported. Open the task to check or start it.' : 'Loading execution…'}</p></li>
        {/if}
      {/each}
    </ul>
  </section>
{/if}
<style>
  section { display: grid; gap: 10px; }
  h2 { font-size: 14px; font-weight: 500; margin: 6px 0 0; }
  ul { display: grid; gap: 10px; list-style: none; padding: 0; margin: 0; }
  li { min-width: 0; }
  p { margin: 6px 0; }
</style>
