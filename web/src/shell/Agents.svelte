<script lang="ts">
  import PageFrame from './PageFrame.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import { executionLabel, myWork } from './aggregate';
  import { model } from './model.svelte';
  const groups = $derived(model.contextOverview ? myWork(model.contextOverview).map(g => ({ ...g, items: g.items.filter(w => !!w.execution) })).filter(g => g.items.length) : []);
</script>
<PageFrame id="agents-h" title="Agents" lede="Reported agent activity in the selected project context.">
  {#if !model.contextOverview}<p role="status">Loading agent activity…</p>
  {:else if !groups.length}<p class="muted">No agent activity reported in this context. Shared projects only show activity you have permission to see.</p>{/if}
  {#each groups as g (g.workspace.id)}
    <section aria-label={g.workspace.name}>
      <WorkspaceTag workspace={g.workspace} />
      <ul>{#each g.items as w (w.id)}<li><button class="btn quiet" type="button" onclick={() => model.goTo(g.workspace.id, w.href)}>{w.title}</button><span>{executionLabel(w.execution ?? '')}{w.report?.stale ? ' · Last reported' : ''}</span></li>{/each}</ul>
    </section>
  {/each}
</PageFrame>
<style>
  section { margin-top: 24px; }
  ul { list-style: none; padding: 0; }
  li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 0; border-bottom: 1px solid var(--border); }
  li span { color: var(--text-2); font-size: 13px; }
</style>
