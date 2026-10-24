<script lang="ts">
  import { SALIE_PLACEMENTS, type SaliePlacements } from './salie-placement';
  let { value, onchange }: { value: SaliePlacements; onchange: (value: SaliePlacements) => void } = $props();
  const labels = { sidebar: 'Sidebar', floating: 'Floating', both: 'Both' };
</script>

<div class="placements">
  {#each [{ key: 'universal', label: 'Universal' }, { key: 'workspace', label: 'Individual and Team' }] as scope (scope.key)}
    <fieldset>
      <legend>{scope.label}</legend>
      <div class="seg" role="group" aria-label="Salie placement in {scope.label}">
        {#each SALIE_PLACEMENTS as placement (placement)}
          <button type="button" aria-pressed={value[scope.key as keyof SaliePlacements] === placement} onclick={() => onchange({ ...value, [scope.key]: placement })}>{labels[placement]}</button>
        {/each}
      </div>
    </fieldset>
  {/each}
</div>

<style>
  .placements { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 240px), 1fr)); gap: 16px; }
  fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
  legend { margin-bottom: 8px; padding: 0; font-size: 13px; font-weight: 500; }
  .seg { display: flex; }
  button { flex: 1; min-height: 36px; }
  @media (pointer: coarse) { button { min-height: 44px; } }
</style>
