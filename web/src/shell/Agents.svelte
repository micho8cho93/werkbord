<script lang="ts">
  import PageFrame from './PageFrame.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import { executionLabel, myWork } from './aggregate';
  import { model } from './model.svelte';
  // `strip`: shown above the person's own sessions (Agents). It is the activity workspaces report in the selected context, as before;
  // the sessions below it are this computer's own, and a Team's members' sessions run on their computers and are not opened here.
  let { strip = false }: { strip?: boolean } = $props();
  const groups = $derived(model.contextOverview
    ? myWork(model.contextOverview).map(g => ({ ...g, items: g.items.filter(w => !!w.execution) })).filter(g => g.items.length)
    : []);
</script>
{#snippet reported()}
  {#each groups as g (g.workspace.id)}
    <section aria-label={g.workspace.name}>
      <WorkspaceTag workspace={g.workspace} />
      <ul>{#each g.items as w (w.id)}<li><button class="btn quiet" type="button" onclick={() => model.goTo(g.workspace.id, w.href)}>{w.title}</button><span>{executionLabel(w.execution ?? '')}{w.report?.stale ? ' · Last reported' : ''}</span></li>{/each}</ul>
    </section>
  {/each}
{/snippet}
{#if strip}
  {#if groups.length}
    <div class="strip" role="region" aria-label="Reported activity">
      <strong>Reported in this context</strong>
      {@render reported()}
    </div>
  {/if}
{:else}
  <PageFrame id="agents-h" title="Agents" lede="Reported agent activity in the selected project context.">
    {#if !model.contextOverview}<p role="status">Loading agent activity…</p>
    {:else if !groups.length}<p class="muted">No agent activity reported in this context. Shared projects only show activity you have permission to see.</p>{/if}
    {@render reported()}
  </PageFrame>
{/if}
<style>
  section { margin-top: 24px; }
  ul { list-style: none; padding: 0; }
  li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 0; border-bottom: 1px solid var(--border); }
  li span { color: var(--text-2); font-size: 13px; }
  .strip { max-height: 30%; overflow-y: auto; padding: 8px 24px; border-bottom: 1px solid var(--border); background: var(--surface); font-size: 13px; }
  .strip section { margin-top: 8px; }
  .strip li { padding: 4px 0; }
</style>
