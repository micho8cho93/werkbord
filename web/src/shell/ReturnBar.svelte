<script lang="ts">
  // A Team workspace's page cannot carry the way back in its own address, so when the person opened it from one of the window's own
  // pages (Work, Calendar, Agents) the window offers that one step back itself, over the page, until they go somewhere else. Personal's
  // pages draw their own breadcrumb from the address (lib/Crumbs.svelte); this is only for the pages that cannot.
  import { model } from './model.svelte';
  const label: Record<string, string> = { inbox: 'Work', calendar: 'Calendar', agents: 'Agents' };
  const shown = $derived(model.page === 'workspace' && !!model.departed && model.departed.workspace === model.current && model.current !== 'personal' && (model.departed.page in label || !!model.departed.from));
</script>
{#if shown && model.departed}
  {@const from = model.departed.from}
  {@const page = model.departed.page as 'inbox' | 'calendar' | 'agents'}
  <nav class="return" aria-label="Breadcrumb" data-testid="return-bar">
    {#if from}
      <button type="button" onclick={() => { model.departed = null; void model.goTo(from.workspace, from.place); }}><span aria-hidden="true">‹ </span>{from.label}</button>
    {:else}
      <button type="button" onclick={() => model.returnTo(page)}><span aria-hidden="true">‹ </span>{label[page]}</button>
    {/if}
  </nav>
{/if}
<style>
  .return { flex: none; padding: 6px 24px; border-bottom: 1px solid var(--border); background: var(--surface); font-size: 13px; }
  button { padding: 2px 0; border: 0; background: none; font: inherit; font-weight: 500; color: var(--text); cursor: pointer; }
  button:hover { text-decoration: underline; }
  button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 2px; }
  @media (max-width: 899px) { .return { padding-inline: 16px; } button { min-height: 44px; } }
</style>
