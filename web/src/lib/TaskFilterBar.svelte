<script lang="ts">
  import { filterFor, pruneFor, setFilter } from './filters.svelte';
  import LabelPicker from './LabelPicker.svelte';
  import { filterActive, noFilter, WORK_MODES } from './labels';
  import { app } from './state.svelte';
  import type { WorkMode } from './types';

  // The label and work-mode filter of a project, shared by its Board and its Timeline. It only narrows
  // what is shown; it never changes a task.

  let { projectId }: { projectId: string } = $props();

  const filter = $derived(filterFor(projectId));

  // A label deleted meanwhile is dropped from the filter, so it cannot hide every task.
  $effect(() => {
    pruneFor(projectId, app.labels);
  });

  const set = (mode: WorkMode | '') => setFilter(projectId, { ...filter, mode });
  const setMatch = (match: 'any' | 'all') => setFilter(projectId, { ...filter, match });
</script>

<div class="bar" role="group" aria-label="Filter by label and work mode">
  {#if app.labels.length}
    <div class="lab"><LabelPicker bind:selected={() => filter.labels, (labels) => setFilter(projectId, { ...filter, labels })} canCreate={false} label="Label" floating /></div>
    {#if filter.labels.length > 1}
      <div class="seg" role="group" aria-label="Match">
        <button aria-pressed={filter.match === 'any'} onclick={() => setMatch('any')}>Any</button>
        <button aria-pressed={filter.match === 'all'} onclick={() => setMatch('all')}>All</button>
      </div>
    {/if}
  {/if}
  <label class="pick" class:on={filter.mode}>
    <span>Who</span>
    <select value={filter.mode} onchange={(e) => set(e.currentTarget.value as WorkMode | '')} aria-label="Filter by who does the work">
      <option value="">anyone</option>
      {#each WORK_MODES as m (m.id)}<option value={m.id}>{m.label}</option>{/each}
    </select>
  </label>
  {#if filterActive(filter)}
    <button class="btn quiet small" onclick={() => setFilter(projectId, noFilter())}>Clear</button>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    align-items: flex-start;
    flex-wrap: wrap;
    gap: 8px;
  }

  .lab {
    min-width: 0;
  }
</style>
