<script lang="ts">
  import { agentName, cardActivity, runElapsed } from '../lib/format';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import RunBadge from '../lib/RunBadge.svelte';
  import { taskHref } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import type { Run } from '../lib/types';

  const titleOf = (run: Run) => app.allTasks.find((t) => t.id === run.taskId)?.title ?? 'Task';
  const projectOf = (run: Run) => app.projects.find((p) => p.id === run.projectId)?.name ?? '';

  /** Runs waiting for a message rather than an answer: they need you, but nothing is blocked on a question. */
  const idle = $derived(app.activeRuns.filter((r) => r.state === 'waiting_for_user' && r.waiting === 'idle'));
  const working = $derived(app.activeRuns.filter((r) => r.state !== 'waiting_for_user'));
  const loaded = $derived(app.connection === 'live');
</script>

<div class="sections">
  <section>
    <h2>Needs you</h2>
    {#if app.questions.length || idle.length}
      <ul class="list">
        {#each app.questions as q (q.id)}
          {@const run = Object.values(app.latestRun).find((r) => r.id === q.runId)}
          <li class="card item ask">
            {#if run}
              <a class="who" href={taskHref(run.taskId)}>{titleOf(run)}</a>
              <p class="muted small">{projectOf(run)} · {agentName(app.agents, run.agentId)}</p>
            {/if}
            <QuestionCard question={q} />
          </li>
        {/each}
        {#each idle as run (run.id)}
          <li class="card item">
            <a class="who" href={taskHref(run.taskId)}>{titleOf(run)}</a>
            <p class="muted small">{projectOf(run)} · {agentName(app.agents, run.agentId)} · finished its turn, waiting for your next message</p>
            {#if cardActivity(run)}<p class="activity">{cardActivity(run)}</p>{/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'Nothing is waiting for you.' : 'Loading…'}</p>
    {/if}
  </section>

  <section>
    <h2>Working</h2>
    {#if working.length}
      <ul class="list">
        {#each working as run (run.id)}
          <li class="card item">
            <a class="who" href={taskHref(run.taskId)}>{titleOf(run)}</a>
            <div class="line">
              <RunBadge {run} />
              <span class="muted small">{projectOf(run)} · {agentName(app.agents, run.agentId)} · {runElapsed(run, app.now)}</span>
            </div>
            {#if cardActivity(run)}<p class="activity">{cardActivity(run)}</p>{/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'No agent is working right now.' : 'Loading…'}</p>
    {/if}
  </section>

  <section>
    <h2>Agents</h2>
    {#if app.agents.length}
      <ul class="list">
        {#each app.agents as a (a.id)}
          <li class="card item row">
            <span>{a.name}{#if a.version}<span class="muted small"> {a.version}</span>{/if}</span>
            <span class="pill" data-ok={a.available}>{a.available ? 'Ready' : (a.detail ?? 'Unavailable')}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'No agents are configured.' : 'Loading…'}</p>
    {/if}
  </section>
</div>

<style>
  .sections {
    display: grid;
    gap: 20px;
    max-width: 760px;
  }

  section {
    display: grid;
    gap: 8px;
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
    gap: 6px;
  }

  .item.ask {
    border-left: 3px solid var(--warn);
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

  .activity {
    font-size: 0.82rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .small {
    font-size: 0.8rem;
  }

  .pill {
    flex: none;
    max-width: 60%;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--surface-2);
    font-size: 0.78rem;
    font-weight: 600;
    text-align: right;
    overflow-wrap: anywhere;
  }

  .pill[data-ok='true'] {
    color: var(--ok);
  }
</style>
