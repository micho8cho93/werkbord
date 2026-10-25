<script lang="ts">
  import { wheres } from './analytics';
  import { model } from './model.svelte';

  // Where to look: Personal and every team, Personal alone, or one team. A place that is no longer there (a team left) is dropped
  // back to everything, not kept as a ghost filter.
  let { where = $bindable('all'), label, testid }: { where: string; label: string; testid: string } = $props();

  const places = $derived(wheres(model.view?.items));
  $effect(() => {
    if (!places.some((p) => p.id === where)) where = 'all';
  });
</script>

<label class="pick"><span>Where</span>
  <select bind:value={where} aria-label={label} data-testid={testid}>
    {#each places as p (p.id)}<option value={p.id}>{p.label}</option>{/each}
  </select>
</label>
