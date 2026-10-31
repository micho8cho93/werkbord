<script lang="ts">
  import { onMount } from 'svelte';
  import PageFrame from './PageFrame.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import { executionLabel, reportedActivity } from './aggregate';
  import { reportedLine } from '../lib/agent-overview';
  import { model } from './model.svelte';
  // `strip`: shown above the person's own sessions (Agents). The sessions below it are this computer's own and are listed there, so the
  // strip carries what is reported and shown nowhere else: the activity of Team workspaces in the selected context, whose members'
  // sessions run on their computers and are not opened here. Without the sessions (the page below), everything reported is listed.
  let { strip = false }: { strip?: boolean } = $props();
  // Reported activity is what a workspace says its members' agents are doing, with when it said so. It is read, never operated from here:
  // opening it goes to the workspace's own page, and there is no input or stop for it in this list.
  let now = $state(Date.now());
  onMount(() => { const t = setInterval(() => (now = Date.now()), 30000); return () => clearInterval(t); });
  const available = (g: { workspace: { state: string } }) => g.workspace.state === 'ready';
  const groups = $derived(model.contextOverview ? reportedActivity(model.contextOverview, strip) : []);
</script>
{#snippet reported()}
  {#each groups as g (g.workspace.id)}
    <section aria-label={g.workspace.name}>
      <WorkspaceTag workspace={g.workspace} />
      <p class="kind" data-testid="reported-note">Reported activity · view only. It runs on a member’s computer.</p>
      <ul>{#each g.items as w (w.id)}{@const r = reportedLine(w.report, now, available(g))}<li data-freshness={r.freshness}><button class="btn quiet" type="button" onclick={() => model.goTo(g.workspace.id, w.href)}>{w.title}</button><span>{executionLabel(w.execution ?? '')} · {r.label}</span></li>{/each}</ul>
    </section>
  {/each}
{/snippet}
{#if strip}
  {#if groups.length}
    <div class="strip" role="region" aria-label="Team activity">
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
  .kind { margin: 4px 0 0; font-size: 12px; color: var(--text-2); }
  li[data-freshness='stale'] span, li[data-freshness='unknown'] span { font-style: italic; }
  li span { color: var(--text-2); font-size: 13px; }
  .strip { max-height: 30%; overflow-y: auto; padding: 8px 24px; border-bottom: 1px solid var(--border); background: var(--surface); font-size: 13px; }
  .strip section { margin-top: 8px; }
  .strip li { padding: 4px 0; }
</style>
