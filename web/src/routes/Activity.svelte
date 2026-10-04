<script lang="ts">
  import { agentName, cardActivity, runElapsed, runStatus, timeAgo } from '../lib/format';
  import { interactionShort, isNotable } from '../lib/policy';
  import RunBadge from '../lib/RunBadge.svelte';
  import { taskHref } from '../lib/router.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import type { Project } from '../lib/types';

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  const titleOf = (id: string) => scope.tasks.find((t) => t.id === id)?.title ?? 'Task';
</script>

<div class="page">
  <p class="muted lead">Every agent run in <strong>{project.name}</strong>, newest first.</p>

  {#if scope.history.length}
    <ul class="list">
      {#each scope.history as run (run.id)}
        {@const status = runStatus(run)}
        <li class="card item" data-tone={status.tone}>
          <a class="who" href={taskHref(project.id, run.taskId)}>{titleOf(run.taskId)}</a>
          <div class="line">
            <RunBadge {run} />
            <span class="muted small">
              {agentName(app.agents, run.agentId)} · {runElapsed(run, app.now)} · {timeAgo(run.createdAt, app.now)}
              {#if isNotable(run.policy)}· {interactionShort(run.policy)}{/if}
            </span>
          </div>
          {#if run.parentRunId}<p class="small muted">Run #{run.attempt} · {run.purpose} from {scope.history.find(r=>r.id===run.parentRunId)?.agentId??'an earlier run'}{run.model?` · ${run.model}`:''}</p>{:else if run.attempt}<p class="small muted">Run #{run.attempt}{run.model?` · ${run.model}`:''}</p>{/if}
          {#if cardActivity(run)}<p class="activity" class:bad={run.state === 'failed'}>{cardActivity(run)}</p>{/if}
        </li>
      {/each}
    </ul>
  {:else}
    <p class="card empty">{scope.loaded ? 'No agent has run in this project yet.' : 'Loading…'}</p>
  {/if}
</div>

<style>
  .page {
    display: grid;
    gap: 12px;
    max-width: 760px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .item {
    padding: 12px;
    display: grid;
    gap: 5px;
    border-left: 3px solid var(--border);
  }

  .item[data-tone='work'],
  .item[data-tone='idle'] {
    border-left-color: var(--accent);
  }

  .item[data-tone='ask'] {
    border-left-color: var(--warn);
  }

  .item[data-tone='block'] {
    border-left-color: var(--block);
  }

  .item[data-tone='bad'] {
    border-left-color: var(--danger);
  }

  .item[data-tone='ok'] {
    border-left-color: var(--ok);
  }

  .who {
    font-weight: 600;
    color: inherit;
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  .who:hover {
    text-decoration: underline;
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 10px;
  }

  .small {
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
</style>
