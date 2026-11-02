<script lang="ts">
  import { onMount } from 'svelte';
  import AgentMark from '../lib/AgentMark.svelte';
  import BrandMark from '../lib/BrandMark.svelte';
  import { codingAgents, connectedTools, usageLines } from '../lib/connected-tools';
  import { ask } from './native';
  import { model } from './model.svelte';
  import type { AgentsAndConnections } from './types';

  // The body of the "Agents & connections" popover, for the person's own (Personal) context. The app reads it once when the popover
  // opens and hands over only names, yes or no, counts and two cost figures; a missing figure reads as missing. This is the person's own
  // setup: nothing here enters a Team report.
  let { project }: { project: string } = $props();
  let reading = $state<AgentsAndConnections | undefined>();
  let loaded = $state(false);
  let failed = $state(false);

  onMount(() => {
    void ask(a => a.AgentsAndConnections(project)).then(r => (reading = r), () => (failed = true)).finally(() => (loaded = true));
  });

  const agents = $derived(codingAgents(reading?.agents ?? []));
  const tools = $derived(connectedTools(reading?.tools ?? []));
  const usage = $derived(usageLines(reading?.usage));
</script>

<div class="tools" data-testid="connected-tools">
  {#if !loaded}<p class="note" role="status">Looking at what is connected…</p>
  {:else if failed}<p class="note" role="alert">Could not read what is connected.</p>
  {:else}
    <p class="heading">Coding agents</p>
    {#if agents.length}
      <ul class="list">
        {#each agents as a (a.id)}
          <li class="row"><AgentMark agentId={a.id} name={a.name} decorative />
            <span class="what"><strong>{a.name}</strong><span class="where">{a.status}{a.active ? ` · ${a.active} active` : ''} · {a.limit}</span></span></li>
        {/each}
      </ul>
    {:else}<p class="note">No coding agent was found on this computer.</p>{/if}

    {#if tools.length}
      <p class="heading">Connected tools</p>
      <ul class="list">
        {#each tools as t (t.id)}
          <li class="row"><BrandMark id={t.service?.id ?? t.id} name={t.service?.name ?? t.name} size={24} />
            <span class="what"><strong>{t.name}</strong><span class="where">{t.state}</span></span></li>
        {/each}
      </ul>
    {/if}

    {#if usage}
      <p class="heading">Usage, last 30 days</p>
      <dl class="usage" data-testid="connected-usage">
        <div><dt>Reported API charges</dt><dd>{usage.reported}</dd></div>
        <div><dt>Estimated API equivalent</dt><dd>{usage.estimated}</dd></div>
      </dl>
      <p class="note">{usage.caveat}</p>
    {/if}
  {/if}
  <p class="foot"><button type="button" class="link" onclick={() => model.go('settings')}>Settings</button></p>
</div>

<style>
  .list { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }
  .row { display: flex; align-items: center; gap: 10px; padding: 6px 8px; }
  .what { display: grid; min-width: 0; }
  .what strong { font-weight: 500; }
  .where { color: var(--text-2); font-size: 12px; }
  .heading { margin: 8px 8px 2px; font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-2); }
  .heading:first-child { margin-top: 0; }
  .note { margin: 4px 8px; font-size: 12px; color: var(--text-2); line-height: 1.5; }
  .usage { margin: 4px 8px; display: grid; gap: 4px; font-size: 12px; }
  .usage div { display: flex; justify-content: space-between; gap: 12px; }
  .usage dt { color: var(--text-2); }
  .usage dd { margin: 0; font-variant-numeric: tabular-nums; }
  .foot { margin: 8px 8px 0; padding-top: 6px; border-top: 1px solid var(--border); }
  .link { border: 0; background: none; padding: 0; color: var(--accent); font: inherit; cursor: pointer; }
  .link:hover { text-decoration: underline; }
  .link:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
</style>
