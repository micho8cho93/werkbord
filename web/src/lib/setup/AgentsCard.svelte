<script lang="ts">
  import { app } from '../state.svelte';
  import SetupCard from './SetupCard.svelte';

 let selected=$state('');
 const selectedAgent=$derived(app.agents.find(a=>a.id===selected) ?? app.agents.find(a=>a.available) ?? app.agents[0]);

  const ready = $derived(app.agents.filter((a) => a.available));
  const status = $derived(ready.length > 0 ? 'done' : 'todo');
  const summary = $derived(ready.length > 0 ? `${ready.length} agent${ready.length===1?'':'s'} ready` : 'None ready yet');

  /** The words for what is wrong with an agent that is not ready. */
  function agentStatus(a: (typeof app.agents)[number]): string {
    if (a.available) return a.signIn === 'signed_in' ? `${a.version ?? ''} · signed in` : (a.version ?? 'ready');
    if (a.installed) return `${a.version ?? ''} · ${a.detail ?? 'cannot be used'}`;
    return 'Not installed';
  }
</script>

<SetupCard title="Coding agents" {status} {summary}>
  <p class="muted">
    Werkbord runs the agents you already have, signed in with your own account. It never asks for an API key and never installs an
    agent without you.
  </p>
  <label for="setup-agent">Coding agent</label>
 <select id="setup-agent" class="select" value={selectedAgent?.id ?? ''} onchange={e=>selected=e.currentTarget.value}>{#each app.agents as a(a.id)}<option value={a.id}>{a.name}{a.available?' · ready':''}</option>{/each}</select>
 <ul class="agents">
    {#each selectedAgent ? [selectedAgent] : [] as a (a.id)}
      <li class="agent" data-ready={a.available}>
        <span class="what">
          <strong>{a.name}</strong>
          <span class="status" data-ready={a.available}>{agentStatus(a)}</span>
        </span>
        {#if !a.available && a.guidance}
          <p class="guidance">{a.guidance}</p>
          {#if a.docsUrl}<a class="small" href={a.docsUrl} target="_blank" rel="noopener noreferrer">Instructions</a>{/if}
        {/if}
      </li>
    {/each}
  </ul>
</SetupCard>

<style>
  .agents {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .agent {
    display: grid;
    gap: 3px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
  }

  .agent[data-ready='true'] {
    border-color: color-mix(in srgb, var(--ok) 40%, var(--border));
  }

  .what {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    align-items: baseline;
  }

  .status {
    font-size: 0.85rem;
    color: var(--text-2);
  }

  .status[data-ready='true'] {
    color: var(--ok-text);
  }

  .guidance {
    font-size: 0.85rem;
    overflow-wrap: anywhere;
  }

  .small {
    font-size: 0.82rem;
  }
</style>
