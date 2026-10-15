<script lang="ts">
  import { sinceShort } from '../board';
  import { agentLabel } from '../execution';
  import { oneLine } from '../format';
  import Icon from '../Icon.svelte';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import { usageLine } from './agents';

  // Work that is finished and waiting for the person to look at it, oldest first, with what the agent said it did. Looking at it
  // happens on the task, where the changes, the Git review and the one action that settles it (merge, or move it on) already are.
  const reviews = $derived(app.overview?.review ?? []);
  const loaded = $derived(app.overview !== null);
</script>

<div class="reviews">
  {#if !loaded}
    <p class="note" role="status">Looking…</p>
  {:else if reviews.length === 0}
    <div class="clear card">
      <span class="dot" data-tone="ok" aria-hidden="true"></span>
      <div>
        <p class="clear-title">Nothing is waiting for review.</p>
        <p class="note">Finished work lands here for you to look at, with what the agent did.</p>
      </div>
    </div>
  {:else}
    <ul>
      {#each reviews as r (r.task.id)}
        {@const run = r.lastRun}
        {@const result = run?.handoff?.results || run?.handoff?.summary || ''}
        {@const files = run?.handoff?.filesChanged?.length ?? 0}
        <li class="card review" data-testid="review">
          <p class="kind"><span class="dot" data-tone="ok" aria-hidden="true"></span><b>Ready for review</b><span class="mm">{r.projectName} · {sinceShort(r.task.updatedAt, app.now)}</span></p>
          <a class="title" href={taskHref(r.task.projectId, r.task.id)}>{r.task.title}</a>
          {#if run}
            <p class="meta mm">
              {agentLabel(app.agents, run.agentId)}{run.branch ? ` · ${run.branch}` : ''}{run.headCommit ? ` · ${run.headCommit.slice(0, 7)}` : ''}{run.uncommitted ? ' · uncommitted changes' : ''}
            </p>
          {/if}
          {#if result}<p class="well">{oneLine(result, 220)}</p>{/if}
          <p class="meta mm">
            {files ? `${files} ${files === 1 ? 'file' : 'files'} changed` : 'No file list reported'}{run && usageLine(run) ? ` · used ${usageLine(run)}` : ''}
          </p>
          <div class="acts">
            <a class="btn small primary" href={taskHref(r.task.projectId, r.task.id)}><Icon name="merge" size={14} />Review</a>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .reviews {
    padding: 4px 16px 24px;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }

  .review {
    display: grid;
    gap: 8px;
    padding: 14px;
  }

  p {
    margin: 0;
  }

  .kind {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 8px;
    font-size: 13px;
  }

  .title {
    font-size: 14px;
    font-weight: 500;
    color: var(--text);
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  .meta,
  .well {
    overflow-wrap: anywhere;
  }

  .acts .btn {
    min-height: 44px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    text-decoration: none;
  }

  .clear {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 16px;
  }

  .clear-title {
    font-weight: 500;
    margin-bottom: 4px;
  }

  .note {
    color: var(--text-2);
    font-size: 13px;
  }
</style>
