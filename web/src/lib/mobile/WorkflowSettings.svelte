<script lang="ts">
  import { untrack } from 'svelte';
  import Sheet from '../Sheet.svelte';
  import { app } from '../state.svelte';
  import { eligibleRunners, executionAgents, modelChoices, resolveFor, validModelName } from '../execution';
  import { workflow, type DeckSlot } from './layout';
  let { slot, onclose, onsave }: { slot: DeckSlot; onclose: () => void; onsave: (slot: DeckSlot) => Promise<void> } = $props();
  let projectId = $state(untrack(() => slot.projectId ?? ''));
  let agent = $state(untrack(() => slot.agent ?? ''));
  let model = $state(untrack(() => slot.model === 'default' ? '' : slot.model ?? ''));
  let custom = $state(false);
  let busy = $state(false);
  let error = $state('');
  const definition = $derived(workflow(slot.button));
  const repositories = $derived(app.projects.filter(p => p.kind !== 'work' && !!p.repoPath));
  const project = $derived(repositories.find(p => p.id === projectId));
  const inherited = $derived(resolveFor({}, project?.execution, app.globalExecution));
  const agents = $derived(executionAgents(app.agents, app.runners, projectId, inherited.runner, true));
  const options = $derived(eligibleRunners(app.runners, projectId, inherited.runner, true).flatMap(r => r.capabilities?.options ?? []).find(o => o.agentId === agent) ?? app.agentOptions.get(agent));
  const choices = $derived(modelChoices(options));
  const problem = $derived(model && !validModelName(model) ? 'Use a valid model name, without spaces or command flags.' : '');
  $effect(() => { if (agent) void app.loadAgentOptions(agent); });
  async function save(e: SubmitEvent) {
    e.preventDefault(); if (!project || problem || busy) return;
    busy = true; error = '';
    try { await onsave({ id: slot.id, button: slot.button, projectId, ...(agent ? { agent } : {}), ...(agent ? { model: model.trim() || 'default' } : {}) }); }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }
</script>

<Sheet title="Configure {definition?.label ?? 'workflow'}" {onclose}>
  <form id="workflow-settings" onsubmit={save}>
    <p>{definition?.description}</p>
    <p class="muted">Runs on Individual through your existing runner and permissions. Changes are reviewed through the task.</p>
    <label>Repository <select class="select" required bind:value={projectId} disabled={busy} onchange={() => { agent = ''; model = ''; }}>
      <option value="">Choose a repository</option>
      {#each repositories as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
      {#if projectId && !project}<option value={projectId}>Repository unavailable — choose another</option>{/if}
    </select></label>
    {#if !repositories.length}<p class="muted">Add a Git repository on your computer first, then return here.</p>{/if}
    <label>Agent <select class="select" bind:value={agent} disabled={busy || !project} onchange={() => { model = ''; custom = false; }}>
      <option value="">Project / controller default{inherited.agent ? ` (${inherited.agent})` : ''}</option>
      {#each agents as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
      {#if agent && !agents.some(a => a.id === agent)}<option value={agent}>{agent} (unavailable)</option>{/if}
    </select></label>
    <label>Model <select class="select" disabled={busy || !agent} value={custom ? '__custom' : model} onchange={e => { if (e.currentTarget.value === '__custom') { custom = true; model = ''; } else { custom = false; model = e.currentTarget.value; } }}>
      <option value="">Agent default</option>
      {#each choices.list.filter(m => m.id !== 'default') as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
      {#if model && !choices.list.some(m => m.id === model) && !custom}<option value={model}>{model}</option>{/if}
      {#if choices.custom}<option value="__custom">Another model…</option>{/if}
    </select></label>
    {#if custom && agent}<label>Model name <input class="input" bind:value={model} disabled={busy} maxlength="100" autocomplete="off" autocapitalize="off" spellcheck="false" /></label>{/if}
    {#if problem}<p class="error" role="alert">{problem}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <p class="muted">Saving configures this button. Tap it on the deck to start work. Hold it, press Shift+F10, or use Edit Deck to configure it again.</p>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={onclose} disabled={busy}>Cancel</button>
    <button class="btn primary" type="submit" form="workflow-settings" disabled={busy || !project || !!problem}>{busy ? 'Saving…' : 'Save configuration'}</button>
  {/snippet}
</Sheet>
<style>
  form { display: grid; gap: 14px; }
  label { display: grid; gap: 6px; }
  p { margin: 0; }
  select, input { min-height: 44px; width: 100%; font-size: 16px; }
</style>
