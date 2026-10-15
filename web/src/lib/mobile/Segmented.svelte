<script lang="ts" generics="T extends string">
  // One choice among a few, as the design system's segmented control, with touch-sized segments. It stands in for a drop-down
  // wherever the phone has a handful of choices (Month, Week, Timeline; Overview, Work, Agents).
  let {
    options,
    value,
    onchange,
    label,
    full = true,
  }: { options: readonly { id: T; label: string; count?: number }[]; value: T; onchange: (id: T) => void; label: string; full?: boolean } = $props();
</script>

<div class="seg phone-seg" class:full role="group" aria-label={label}>
  {#each options as o (o.id)}
    <button type="button" aria-pressed={value === o.id} onclick={() => onchange(o.id)}>
      {o.label}{#if o.count !== undefined}<span class="mono n" aria-hidden="true">{o.count}</span>{/if}
    </button>
  {/each}
</div>

<style>
  .phone-seg.full {
    display: flex;
    width: 100%;
  }

  .phone-seg button {
    flex: 1;
    min-width: 0;
    padding: 0 4px;
    justify-content: center;
    white-space: nowrap;
    min-height: 40px;
    height: auto;
    font-size: 13.5px;
    gap: 5px;
  }

  .n {
    font-size: 11.5px;
    color: var(--text-2);
  }
</style>
