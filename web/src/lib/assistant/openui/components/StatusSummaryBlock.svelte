<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  let { props }: ComponentRenderProps<{ backlog?: number; doing?: number; review?: number; done?: number; asOf?: string }> = $props();
  const states = $derived([
    { name: 'Backlog', count: props.backlog ?? 0, state: 'backlog' },
    { name: 'In progress', count: props.doing ?? 0, state: 'doing' },
    { name: 'Review', count: props.review ?? 0, state: 'review' },
    { name: 'Done', count: props.done ?? 0, state: 'done' },
  ]);
</script>

<section class="summary" aria-label="Current task status counts">
  <dl>
    {#each states as item (item.state)}
      <div class="state" data-state={item.state}><dt>{item.name}</dt><dd>{item.count}</dd></div>
    {/each}
  </dl>
  <SnapshotStamp asOf={props.asOf} />
</section>

<style>
  dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; border-block: 1px solid var(--border); }
  .state { min-width: 0; padding: 9px 12px; border-right: 1px solid var(--border); }
  .state:last-child { border-right: 0; }
  dt { color: var(--text-2); font-size: 12px; }
  dd { margin: 3px 0 0; font: 600 17px/1.3 var(--mono); font-variant-numeric: tabular-nums; }
  .state[data-state='doing'] dd { color: var(--accent); }
  .state[data-state='review'] dd { color: var(--warn-text); }
  .state[data-state='done'] dd { color: var(--ok-text); }
  @media (max-width: 520px) {
    dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .state:nth-child(2) { border-right: 0; }
    .state:nth-child(n + 3) { border-top: 1px solid var(--border); }
  }
</style>
