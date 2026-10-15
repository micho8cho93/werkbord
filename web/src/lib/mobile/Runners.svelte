<script lang="ts">
  import Icon from '../Icon.svelte';
  import RunnersPanel from '../RunnersPanel.svelte';
  import { app } from '../state.svelte';
  import { matchesRunner, runnerCard, type RunnerFilter } from './runners';
  import Segmented from './Segmented.svelte';

  // The person's own runners: the machines that carry out their agents. The cards say how each stands (reachable, how much of it is in
  // use, what it reports of itself); managing them (pairing, capacity, which projects, removing) is the panel Settings already has, with the
  // same controls and the same rules, below. A runner belongs to the person who paired it; nothing here reaches anyone else's machine.
  let filter = $state<RunnerFilter>('all');
  const cards = $derived(app.runners.filter((r) => matchesRunner(r, filter)).map(runnerCard));
  const n = (f: RunnerFilter) => app.runners.filter((r) => matchesRunner(r, f)).length;
  const filters = $derived([
    { id: 'all', label: 'All', count: n('all') },
    { id: 'online', label: 'Online', count: n('online') },
    { id: 'offline', label: 'Offline', count: n('offline') },
  ] as const);
</script>

<div class="runners">
  <Segmented options={filters} value={filter} onchange={(id: RunnerFilter) => (filter = id)} label="Show runners" />

  {#if app.runners.length === 0}
    <p class="note">No runners are connected yet. Add one below.</p>
  {:else if cards.length === 0}
    <p class="note">No runner is {filter}.</p>
  {/if}
  <ul>
    {#each cards as c (c.id)}
      <li class="card runner" data-testid="runner-card" data-tone={c.tone}>
        <div class="head">
          <span class="tile" aria-hidden="true"><Icon name="runner" size={20} /></span>
          <span class="who">
            <span class="name">{c.name}</span>
            {#if c.spec}<span class="spec mm">{c.spec}</span>{/if}
          </span>
          <span class="state mm"><span class="dot" data-tone={c.tone} aria-hidden="true"></span>{c.state}</span>
        </div>
        {#if c.online}
          <dl class="stats">
            <div><dt>Run slots</dt><dd>{c.slots}</dd></div>
            {#if c.cpu}<div><dt>CPU</dt><dd>{c.cpu}</dd></div>{/if}
            {#if c.ram}<div><dt>Memory</dt><dd>{c.ram}</dd></div>{/if}
            {#if c.storage}<div><dt>Storage</dt><dd>{c.storage}</dd></div>{/if}
          </dl>
        {/if}
        {#if c.note}<p class="well">{c.note}</p>{/if}
      </li>
    {/each}
  </ul>

  <section aria-labelledby="run-manage">
    <h2 id="run-manage" class="lab">Manage</h2>
    <RunnersPanel />
  </section>
</div>

<style>
  .runners {
    display: grid;
    gap: 14px;
    padding: 4px 16px 24px;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }

  .note {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }

  section {
    display: grid;
    gap: 8px;
  }

  h2 {
    margin: 0;
  }

  .runner {
    display: grid;
    gap: 12px;
    padding: 14px;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 12px;
  }

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
    color: var(--text-2);
  }

  .runner[data-tone='work'] .tile,
  .runner[data-tone='ok'] .tile {
    color: var(--accent);
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }

  .name {
    font-size: 15px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .spec {
    overflow-wrap: anywhere;
  }

  .state {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: none;
  }

  .stats {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(5.5rem, 1fr));
    gap: 10px;
    margin: 0;
    padding-top: 10px;
    border-top: 1px solid var(--border);
  }

  .stats div {
    display: grid;
    gap: 2px;
  }

  dt {
    font-family: var(--mono);
    font-size: 11.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  dd {
    margin: 0;
    font-size: 14px;
    font-weight: 500;
  }
</style>
