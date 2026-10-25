<script lang="ts">
  import BlockEmpty from './BlockEmpty.svelte';
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ label?: string; value?: number; total?: number; detail?: string; asOf?: string }> = $props();
  const hasTotal = $derived((props.total ?? 0) > 0);
  const value = $derived(Math.max(0, props.value ?? 0));
  const total = $derived(Math.max(0, props.total ?? 0));
</script>

<section class="progress-block" aria-label={`${props.label ?? 'Progress'}: ${hasTotal ? `${value} of ${total}` : 'no total recorded'}`}>
  <div class="heading"><h3>{props.label ?? 'Progress'}</h3>{#if hasTotal}<span class="count">{value} of {total}</span>{/if}</div>
  {#if hasTotal}
    <progress aria-label={props.label ?? 'Progress'} max={total} value={value}>{value} of {total}</progress>
  {:else}
    <BlockEmpty>No total is recorded for this view.</BlockEmpty>
  {/if}
  {#if props.detail}<p class="detail">{props.detail}</p>{/if}
  <SnapshotStamp asOf={props.asOf} />
</section>

<style>
  .progress-block { display: grid; gap: 7px; min-width: 0; }
  .heading { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 4px 12px; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .count { color: var(--text-2); font: 12px var(--mono); font-variant-numeric: tabular-nums; }
  progress { width: 100%; height: 9px; overflow: hidden; border: 0; border-radius: 2px; background: var(--surface-2); accent-color: var(--accent); }
  progress::-webkit-progress-bar { background: var(--surface-2); }
  progress::-webkit-progress-value { background: var(--accent); }
  progress::-moz-progress-bar { background: var(--accent); }
  .detail { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.45; }
</style>
