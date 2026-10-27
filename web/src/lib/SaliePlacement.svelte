<script lang="ts">
  import { SALIE_PLACEMENTS, everywhere, unifiedPlacement, type SaliePlacement, type SaliePlacements } from './salie-placement';
  let { value, onchange }: { value: SaliePlacements; onchange: (value: SaliePlacements) => void } = $props();
  const labels: Record<SaliePlacement, string> = { sidebar: 'Sidebar', floating: 'Floating', both: 'Both' };
  const current = $derived(unifiedPlacement(value));
</script>

<div class="seg" role="group" aria-label="Where Salie appears">
  {#each SALIE_PLACEMENTS as placement (placement)}
    <button type="button" aria-pressed={current === placement} onclick={() => onchange(everywhere(placement))}>{labels[placement]}</button>
  {/each}
</div>

<style>
  .seg { display: flex; max-width: 360px; }
  button { flex: 1; min-height: 36px; }
  @media (pointer: coarse) { button { min-height: 44px; } }
</style>
