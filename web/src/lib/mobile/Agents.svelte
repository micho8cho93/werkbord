<script lang="ts">
  import Icon from '../Icon.svelte';
  import { app } from '../state.svelte';
  import { agentCards, agentSessions, matchesFilter, type AgentFilter } from './agents';
  import Segmented from '../Segmented.svelte';
  import SessionRow from './SessionRow.svelte';

  // Each agent as a card: whether it is working, waiting for you or idle, and what it is doing, with the controls of its session (open,
  // reply, stop). It reads the controller's own overview and changes nothing except a stop the person confirms.
  const cards = $derived(agentCards(app.agents.filter(a=>a.installed || a.available), app.overview));
  const failed = $derived(agentSessions(app.overview).failed);
  const loaded = $derived(app.overview !== null);
  const stale = $derived(app.connection !== 'live');
  let filter = $state<AgentFilter>('all');
  const n = (f: AgentFilter) => cards.filter((c) => matchesFilter(c, f)).length;
  const filters = $derived([
    { id: 'all', label: 'All', count: n('all') },
    { id: 'running', label: 'Running', count: n('running') },
    { id: 'waiting', label: 'Waiting', count: n('waiting') },
    { id: 'idle', label: 'Idle', count: n('idle') },
  ] as const);
  const shown = $derived(cards.filter((c) => matchesFilter(c, filter)));
  const tone = (s: string) => (s === 'Working' ? 'work' : s === 'Waiting for you' ? 'ask' : s === 'Idle' ? 'ok' : 'neutral');
</script>

<div class="agents">
  {#if !loaded}
    <p class="note" role="status">Looking…</p>
  {:else}
    {#if stale}<p class="note" role="status">Live updates are not connected. These are the last reported states.</p>{/if}
    <Segmented options={filters} value={filter} onchange={(id: AgentFilter) => (filter = id)} label="Show agents" />
    {#if cards.length === 0}
      <p class="note">No installed agents were found. Open Settings → Agents for supported CLIs and setup instructions.</p>
    {:else if shown.length === 0}
      <p class="note">No agent is {filter === 'waiting' ? 'waiting for you' : filter}.</p>
    {/if}
    <ul class="cards">
      {#each shown as c (c.id)}
        <li class="card agent" data-testid="agent-card" data-state={c.state} data-tone={tone(c.state)}>
          <div class="head">
            <span class="tile" aria-hidden="true"><Icon name="activity" size={20} /></span>
            <span class="who">
              <span class="name">{c.name}</span>
              <span class="state mm"><span class="dot" data-tone={tone(c.state)} aria-hidden="true"></span>{c.state}{c.sessions.length > 1 ? ` · ${c.sessions.length} sessions` : ''}</span>
            </span>
          </div>
          {#if c.guidance}<p class="note">{c.guidance}</p>{/if}
          {#if c.state === 'Idle'}<p class="note">Ready. Start it on a task from a project's Board.</p>{/if}
          {#each c.sessions as x (x.run.id)}<ul class="inside"><SessionRow {x} bare /></ul>{/each}
        </li>
      {/each}
    </ul>
    {#if failed.length}
      <section aria-labelledby="ag-failed">
        <h2 id="ag-failed" class="lab">Failed <span class="chip">{failed.length}</span></h2>
        <ul class="cards">{#each failed as x (x.run.id)}<SessionRow {x} />{/each}</ul>
      </section>
    {/if}
  {/if}
</div>

<style>
  .agents {
    display: grid;
    gap: 14px;
    padding: 4px 16px 24px;
  }

  section {
    display: grid;
    gap: 8px;
  }

  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .cards {
    display: grid;
    gap: 10px;
  }

  .note {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }

  .agent {
    display: grid;
    gap: 10px;
    padding: 14px;
  }

  .agent[data-tone='ask'] {
    border-color: color-mix(in srgb, var(--warn) 55%, var(--border));
    box-shadow:
      var(--card-sh),
      0 0 0 3px var(--amber-ring);
  }

  .head {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  /* The agent's tile: the design system's tray with the same icon the Agents key has. */
  .tile {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: 10px;
    background: var(--surface-2);
    box-shadow: var(--tray-sh);
    color: var(--accent);
  }

  .agent[data-tone='ask'] .tile {
    color: var(--warn);
  }

  .agent[data-tone='ok'] .tile,
  .agent[data-tone='neutral'] .tile {
    color: var(--text-2);
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .name {
    font-size: 15px;
    font-weight: 600;
  }

  .state {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .inside {
    display: grid;
  }
</style>
