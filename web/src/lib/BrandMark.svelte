<script lang="ts">
  import { BRAND_MARKS } from './brand-marks';

  // A service's mark on a white tile, in the service's own colour, or its first letter when no mark is held. The tile is white in
  // every style and theme so that a dark mark (GitHub, Notion) stays readable on a dark page.
  let { id, name, size = 36 }: { id: string; name: string; size?: number } = $props();
  const mark = $derived(BRAND_MARKS[id]);
</script>

<span class="mark" class:letter={!mark} style:--size="{size}px" role="img" aria-label={name}>
  {#if mark}
    <svg viewBox="0 0 24 24" width={size * 0.58} height={size * 0.58} aria-hidden="true"><path d={mark.path} fill={mark.colour} /></svg>
  {:else}{name.slice(0, 1)}{/if}
</span>

<style>
  .mark { flex: none; display: inline-grid; place-items: center; width: var(--size); height: var(--size); border-radius: calc(var(--size) * 0.24); background: #fff; border: 1px solid var(--tray-edge); color: #202329; font-size: calc(var(--size) * 0.46); font-weight: 600; line-height: 1; }
  .mark.letter { background: var(--tray); color: var(--text); }
</style>
