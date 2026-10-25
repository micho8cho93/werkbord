<script lang="ts">
  import BlockEmpty from './BlockEmpty.svelte';
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  type WorkflowItem = { id?: string; title?: string; state?: 'backlog' | 'doing' | 'review' | 'done'; runState?: string; dependsOn?: string[]; blocker?: string };
  let { props }: ComponentRenderProps<{ title?: string; items?: WorkflowItem[]; asOf?: string; note?: string }> = $props();
  const items = $derived(props.items ?? []);
  const byId = $derived(new Map(items.filter((item) => item.id).map((item) => [item.id!, item.title ?? item.id!])));

  function dependencyName(id: string): string { return byId.get(id) ?? `Unlisted task ${id}`; }
</script>

<section class="workflow">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if items.length === 0}
    <BlockEmpty>No task relationships are recorded in this view.</BlockEmpty>
  {:else}
    <ol class="nodes">
      {#each items as item, i (`${item.id ?? item.title}-${i}`)}
        <li class="node" data-state={item.state ?? 'backlog'}>
          <span class="connector" aria-hidden="true"></span>
          <div class="body">
            <div class="title-row"><strong>{item.title ?? 'Untitled task'}</strong><span class="task-state">{item.state ?? 'Task state not reported'}</span></div>
            {#if item.runState}<p class="run">Latest run: {item.runState}</p>{/if}
            {#if item.dependsOn?.length}<p class="depends">Depends on {item.dependsOn.map(dependencyName).join(', ')}</p>{/if}
            {#if item.blocker}<p class="blocker"><strong>Blocked</strong><span>{item.blocker}</span></p>{/if}
          </div>
        </li>
      {/each}
    </ol>
  {/if}
  {#if props.note}<p class="note">{props.note}</p>{/if}
</section>

<style>
  .workflow { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .nodes { display: grid; list-style: none; margin: 0; padding: 0; }
  .node { position: relative; display: grid; grid-template-columns: 16px minmax(0, 1fr); gap: 10px; min-width: 0; padding: 7px 0; }
  .node:not(:last-child)::before { position: absolute; top: 18px; bottom: -7px; left: 7px; width: 1px; background: var(--border); content: ''; }
  .connector { z-index: 1; width: 9px; height: 9px; margin: 5px 0 0 3px; border: 2px solid var(--text-2); border-radius: 50%; background: var(--surface); }
  .node[data-state='doing'] .connector { border-color: var(--accent); background: var(--accent); }
  .node[data-state='review'] .connector { border-color: var(--warn); background: var(--warn); }
  .node[data-state='done'] .connector { border-color: var(--ok); background: var(--ok); }
  .body { min-width: 0; }
  .title-row { display: flex; flex-wrap: wrap; align-items: baseline; gap: 3px 9px; }
  .title-row strong { font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
  .task-state, .run, .depends { color: var(--text-2); font-size: 12px; }
  .run, .depends { margin: 3px 0 0; overflow-wrap: anywhere; }
  .blocker { display: flex; flex-wrap: wrap; gap: 4px 8px; margin: 5px 0 0; color: var(--block); font-size: 12px; line-height: 1.4; }
  .blocker strong { font-weight: 600; }
  .note { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.45; }
</style>
