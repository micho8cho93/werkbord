<script lang="ts">
  import type { Task } from './types';

  // What a task waits for. These are the same dependencies the scheduler honours (an agent is not
  // started before they finish), shown here as the timeline's arrows. Choosing them never moves a date.

  let { tasks, taskId, selected = $bindable() }: { tasks: readonly Task[]; taskId: string; selected: string[] } = $props();

  let search = $state('');
  const q = $derived(search.trim().toLowerCase());
  const options = $derived(
    tasks.filter((t) => t.id !== taskId && (!t.archivedAt || selected.includes(t.id)) && (!q || t.title.toLowerCase().includes(q) || selected.includes(t.id))),
  );

  function toggle(id: string, on: boolean) {
    selected = on ? [...selected, id] : selected.filter((x) => x !== id);
  }
</script>

<fieldset class="group">
  <legend>Depends on <span class="opt">must finish first</span></legend>
  {#if tasks.length > 6}<input class="input" placeholder="Find a task" bind:value={search} aria-label="Find a task" />{/if}
  <ul class="opts">
    {#each options as t (t.id)}
      <li>
        <label class="opt-row">
          <input type="checkbox" checked={selected.includes(t.id)} onchange={(e) => toggle(t.id, e.currentTarget.checked)} />
          <span class="t">{t.title}</span>
          {#if t.archivedAt}<span class="tag">closed</span>{:else if t.state === 'done'}<span class="tag">done</span>{/if}
        </label>
      </li>
    {:else}
      <li class="none">{tasks.length > 1 ? 'No task by that name.' : 'No other tasks in this project yet.'}</li>
    {/each}
  </ul>
</fieldset>

<style>
  .group {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }

  legend {
    padding: 0;
    margin-bottom: 6px;
    font-size: 13px;
    font-weight: 500;
  }

  .opt {
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--text-2);
    margin-left: 4px;
  }

  .opts {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 160px;
    overflow: auto;
    display: grid;
  }

  .opt-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 30px;
    font-weight: 400;
    cursor: pointer;
  }

  .t {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  .none {
    font-size: 12px;
    color: var(--text-2);
  }
</style>
