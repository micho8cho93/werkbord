<script lang="ts">
  import { layout } from '../motion';
  import { app } from '../state.svelte';
  import Segmented from '../Segmented.svelte';
  import { upcomingNote, workGroups, type WorkTab } from './work';
  import WorkRow from './WorkRow.svelte';
  import { projectsLoaded, tasksOf, warmProjects } from './work.svelte';

  // The person's own work across every project: what is theirs to do, what is in progress, what is coming up and what changed lately.
  // It reads the tasks each project holds and opens each one in its own page, so every action is the one the board already offers.
  $effect(() => warmProjects());

  const groups = $derived(workGroups(tasksOf(), app.now));
  const ready = $derived(projectsLoaded());
  const tabs = $derived([
    { id: 'yours', label: 'Yours', count: groups.yours.length },
    { id: 'active', label: 'In progress', count: groups.active.length },
    { id: 'upcoming', label: 'Upcoming', count: groups.upcoming.length },
    { id: 'recent', label: 'Recent' },
  ] as const);
  let tab = $state<WorkTab | ''>('');
  const current = $derived<WorkTab>(tab || (groups.active.length ? 'active' : groups.yours.length ? 'yours' : groups.upcoming.length ? 'upcoming' : 'recent'));
  const empty: Record<WorkTab, string> = {
    yours: 'Nothing is waiting on you. Work that is yours or shared, and not done, appears here.',
    active: 'Nothing is in progress.',
    upcoming: 'Nothing is planned or scheduled for the next two weeks.',
    recent: 'No tasks yet.',
  };
</script>

<div class="work" use:layout={{ revision: tasksOf().map(t => `${t.id}:${t.version}`).join('|'), enabled: ready && app.connection === 'live', entries: true }}>
  <Segmented options={tabs} value={current} onchange={(id: WorkTab) => (tab = id)} label="My Work" />

  {#if !ready}
    <p class="note" role="status">Loading the work of every project…</p>
  {:else if current === 'yours'}
    {@render list(groups.yours.map((task) => ({ task, note: '' })))}
  {:else if current === 'active'}
    {@render list(groups.active.map((task) => ({ task, note: '' })))}
  {:else if current === 'upcoming'}
    {@render list(groups.upcoming.map((u) => ({ task: u.task, note: upcomingNote(u.item, app.now) })))}
  {:else}
    {@render list(groups.recent.map((task) => ({ task, note: '' })))}
  {/if}
</div>

{#snippet list(rows: { task: import('../types').Task; note: string }[])}
  {#if rows.length === 0}
    <p class="note">{empty[current]}</p>
  {:else}
    <ul>
      {#each rows as r (r.task.id)}<li data-motion-key={r.task.id}><WorkRow task={r.task} note={r.note} /></li>{/each}
    </ul>
  {/if}
{/snippet}

<style>
  .work {
    display: grid;
    gap: 12px;
    padding: 4px 16px 24px;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .note {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }
</style>
