<script lang="ts">
  import { api } from '../lib/api';
  import { app } from '../lib/state.svelte';
  import type { Agent, Question, Run } from '../lib/types';

  let runs = $state<Run[]>([]);
  let questions = $state<Question[]>([]);
  let agents = $state<Agent[]>([]);
  let loaded = $state(false);

  // Reload whenever the event stream reports a change.
  $effect(() => {
    void app.revision;
    Promise.all([api.listActiveRuns(), api.listPendingQuestions(), api.listAgents()]).then(
      ([r, q, a]) => {
        runs = r;
        questions = q;
        agents = a;
        loaded = true;
      },
      (err) => app.handleError(err),
    );
  });

  const runLabel: Record<string, string> = {
    starting: 'Starting',
    running: 'Running',
    waiting_for_user: 'Waiting for you',
  };
</script>

<div class="sections">
  <section>
    <h2>Needs you</h2>
    {#if questions.length}
      <ul class="list">
        {#each questions as q (q.id)}
          <li class="card item">
            <p>{q.prompt}</p>
            <p class="muted small">Run <code>{q.runId}</code></p>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'No questions waiting.' : 'Loading…'}</p>
    {/if}
  </section>

  <section>
    <h2>Active runs</h2>
    {#if runs.length}
      <ul class="list">
        {#each runs as r (r.id)}
          <li class="card item row">
            <span><code>{r.id}</code> · {r.agentId}</span>
            <span class="pill">{runLabel[r.state] ?? r.state}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'No runs in progress.' : 'Loading…'}</p>
    {/if}
  </section>

  <section>
    <h2>Agents</h2>
    {#if agents.length}
      <ul class="list">
        {#each agents as a (a.id)}
          <li class="card item row">
            <span>{a.name}</span>
            <span class="pill" data-ok={a.available}>{a.available ? 'Ready' : (a.detail ?? 'Unavailable')}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">
        {loaded ? 'No agent adapters are installed in this build yet. Agent execution is coming in a later phase.' : 'Loading…'}
      </p>
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
    gap: 4px;
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
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--surface-2);
    font-size: 0.78rem;
    font-weight: 600;
  }

  .pill[data-ok='true'] {
    color: var(--ok);
  }
</style>
