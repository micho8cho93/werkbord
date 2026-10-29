<script lang="ts" generics="T extends string">
  // One choice among a few, as the design system's segmented control (`.seg`). It stands in for a drop-down wherever there is a handful
  // of choices: with touch-sized segments across the width on a phone (Month, Week), `compact` for a page of the desktop
  // window (a kind of review, a view of the analytics).
  let {
    options,
    value,
    onchange,
    label,
    full = true,
    compact = false,
  }: { options: readonly { id: T; label: string; count?: number }[]; value: T; onchange: (id: T) => void; label: string; full?: boolean; compact?: boolean } = $props();
</script>

<div class="seg" class:phone-seg={!compact} class:full={!compact && full} role="group" aria-label={label}>
  {#each options as o (o.id)}
    <button type="button" aria-pressed={value === o.id} onclick={() => onchange(o.id)} data-id={o.id}>
      {o.label}{#if o.count !== undefined}<span class="mono segment-count">{o.count}</span>{/if}
    </button>
  {/each}
</div>
