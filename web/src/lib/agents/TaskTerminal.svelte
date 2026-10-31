<script lang="ts">
  import { onMount } from 'svelte';
  import { app } from '../state.svelte';
  import { openAgents } from '../agent-nav';
  import { terminalKey } from '../agent-sessions';
  import { connectedAgents, nativeCLILaunchers } from '../assistant/connected-agents';
  import { terminalApi } from '../assistant/terminal';
  import { selectionApi, type CatalogAgent } from '../selection';
  import { isPhone } from '../phones/capability';
  import type { Origin } from '../origin';
  import Icon from '../Icon.svelte';

  // "Work on this in a terminal": the agent's own command line, opened in the task's repository and linked to the task, so the person
  // can type to the real CLI without a second window. It starts the same terminal Agents' New session starts (same permissions, same
  // limits, not a sandbox); the structured run stays what Start agent does. Shown only where a terminal can be opened at all.
  let { projectId, taskId, repo, from, disabled = false }: { projectId: string; taskId: string; repo: boolean; from: readonly Origin[]; disabled?: boolean } = $props();

  let launchers = $state<CatalogAgent[]>([]);
  let choice = $state('');
  let busy = $state(false);
  let error = $state('');

  onMount(() => {
    if (app.remote || isPhone()) return;
    void (async () => {
      try {
        const [catalog, settings] = await Promise.all([selectionApi.catalog(), selectionApi.settings()]);
        launchers = connectedAgents(catalog, settings.agents).filter(a => nativeCLILaunchers.has(a.id));
        choice = launchers[0]?.id ?? '';
      } catch { /* Fail closed: Settings lists what is connected and why not. */ }
    })();
  });

  async function open() {
    const agent = launchers.find(a => a.id === choice);
    if (!agent?.interactive || busy) return;
    busy = true; error = '';
    try {
      const made = await terminalApi.create(projectId, agent.id, agent.interactive.device, taskId);
      openAgents(terminalKey(made.id), from);
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
</script>

{#if repo && launchers.length}
  <span class="rel" data-testid="task-terminal">
    <span class="k">Terminal</span>
    {#if launchers.length > 1}<label class="sr-only" for="task-terminal-agent">Agent</label>
    <select id="task-terminal-agent" class="select" bind:value={choice} disabled={busy}>{#each launchers as a (a.id)}<option value={a.id}>{a.name}</option>{/each}</select>{/if}
    <button class="btn small" type="button" disabled={disabled || busy} onclick={open}><Icon name="runs" size={13} />{busy ? 'Opening…' : 'Work on this in a terminal'}</button>
    {#if error}<span class="mm" role="alert">{error}</span>{/if}
  </span>
{/if}

<style>
  .rel { display: inline-flex; align-items: center; gap: 8px; color: var(--text); min-width: 0; max-width: 100%; font-size: 13px; }
  .k { font-family: var(--mono); font-size: 11px; text-transform: uppercase; letter-spacing: 0.14em; color: var(--text-2); }
  .mm { font-family: var(--mono); font-size: 12px; color: var(--danger-text); }
  @media (max-width: 899px) { .rel { min-height: 44px; } }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .select { width: auto; max-width: 160px; }
</style>
