<script lang="ts">
  import Icon from '../Icon.svelte';
  import { isPhone } from '../phones/capability';
  import { globalHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import { matchesRunner, runnerCard, type RunnerFilter } from './runners';
  import Segmented from './Segmented.svelte';

  // One connection card per runner. Computer access links to the existing Settings
  // controls; enrolled phones can inspect availability without seeing management actions.
  let filter = $state<RunnerFilter>('all');
  const cards = $derived(app.runners.filter((r) => matchesRunner(r, filter)).map(r => runnerCard(r, app.connection === 'live')));
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
    <p class="note">No runners are connected yet. Connect one in Settings on your computer.</p>
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
            {#if c.cpu}<div><dt>CPU</dt><dd>{c.cpu}</dd></div>{/if}
            {#if c.ram}<div><dt>Memory</dt><dd>{c.ram}</dd></div>{/if}
            {#if c.storage}<div><dt>Storage</dt><dd>{c.storage}</dd></div>{/if}
          </dl>
        {/if}
        {#if c.note}<p class="well">{c.note}</p>{/if}
      </li>
    {/each}
  </ul>

  {#if !isPhone()}<a class="btn" href="{globalHref('settings')}?runners">Manage runners</a>{/if}
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
