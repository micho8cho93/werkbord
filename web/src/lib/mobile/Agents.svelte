<script lang="ts">
  import { agentSessions, fleet } from './agents';
  import { app } from '../state.svelte';
  import SessionRow from './SessionRow.svelte';

  // Every agent session across the projects, the ones that need the person first, and the agents installed on this computer with what
  // each is doing. It reads the controller's own overview and changes nothing except a stop the person confirms.
  const sessions = $derived(agentSessions(app.overview));
  const agents = $derived(fleet(app.agents, app.overview));
  const loaded = $derived(app.overview !== null);
  const none = $derived(sessions.needs.length + sessions.running.length + sessions.failed.length === 0);
  const stale = $derived(app.connection !== 'live');
</script>

<div class="agents">
  {#if !loaded}
    <p class="note" role="status">Looking…</p>
  {:else}
    {#if stale}<p class="note" role="status">Live updates are not connected. These are the last reported states.</p>{/if}
    {#if none}
      <p class="note">No agent sessions are running or waiting. Start an agent on a task from its Board.</p>
    {/if}
    {#if sessions.needs.length}
      <section aria-labelledby="ag-needs">
        <h2 id="ag-needs" class="lab">Waiting for you <span class="num pend">{sessions.needs.length}</span></h2>
        <ul>{#each sessions.needs as x (x.run.id)}<SessionRow {x} />{/each}</ul>
      </section>
    {/if}
    {#if sessions.running.length}
      <section aria-labelledby="ag-run">
        <h2 id="ag-run" class="lab">Running <span class="num">{sessions.running.length}</span></h2>
        <ul>{#each sessions.running as x (x.run.id)}<SessionRow {x} />{/each}</ul>
      </section>
    {/if}
    {#if sessions.failed.length}
      <section aria-labelledby="ag-failed">
        <h2 id="ag-failed" class="lab">Failed <span class="chip">{sessions.failed.length}</span></h2>
        <ul>{#each sessions.failed as x (x.run.id)}<SessionRow {x} />{/each}</ul>
      </section>
    {/if}
    <section aria-labelledby="ag-fleet">
      <h2 id="ag-fleet" class="lab">On this computer</h2>
      {#if agents.length === 0}
        <p class="note">No agents were found. Install one (Claude Code or Codex) on the computer that runs Werkbord.</p>
      {:else}
        <ul class="fleet card">
          {#each agents as e (e.agent.id)}
            <li data-testid="fleet-agent">
              <span class="who">
                <span class="name">{e.agent.name}</span>
                <span class="meta mm">{e.agent.version ? `v${e.agent.version}` : ''}{e.agent.available ? '' : (e.agent.guidance ?? e.agent.detail ?? '')}</span>
              </span>
              <span class="state mm" data-state={e.state}><span class="dot" data-tone={e.state === 'Working' ? 'work' : e.state === 'Waiting for you' ? 'ask' : e.state === 'Idle' ? 'ok' : 'neutral'}></span>{e.state}{e.active > 1 ? ` · ${e.active}` : ''}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</div>

<style>
  .agents {
    display: grid;
    gap: 18px;
    padding: 4px 16px 24px;
  }

  section {
    display: grid;
    gap: 8px;
  }

  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }

  .note {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }

  .fleet {
    gap: 0;
  }

  .fleet li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 52px;
    padding: 8px 14px;
    border-bottom: 1px solid var(--border);
  }

  .fleet li:last-child {
    border-bottom: 0;
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .name {
    font-size: 14px;
    font-weight: 500;
  }

  .meta {
    overflow-wrap: anywhere;
  }

  .state {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: none;
  }
</style>
