<script lang="ts">
  import { untrack } from 'svelte';
  import Sheet from '../Sheet.svelte';
  import { app } from '../state.svelte';
  import { eligibleRunners, executionAgents, modelChoices, reasoningChoices, resolveFor, validModelName } from '../execution';
  import { workflow, type DeckSlot } from './layout';
  let { onboarding = false, slot, onclose, onsave, onrun, onpreview }: { onpreview?: () => void; onboarding?: boolean; slot: DeckSlot; onclose: () => void; onsave: (slot: DeckSlot) => Promise<void>; onrun?: (slot: DeckSlot) => Promise<void> } = $props();
  let agent = $state(untrack(() => slot.agent ?? ''));
  let model = $state(untrack(() => slot.model === 'default' ? '' : slot.model ?? ''));
  let target = $state<NonNullable<DeckSlot['target']>>(untrack(() => slot.target ?? (slot.projectId ? 'selected' : 'current')));
  let projectIds = $state<string[]>(untrack(() => slot.projectIds ?? (slot.projectId ? [slot.projectId] : [])));
  let reasoning = $state(untrack(() => slot.reasoning ?? ''));
  let runner = $state(untrack(() => slot.runner ?? ''));
  let output = $state<NonNullable<DeckSlot['output']>>(untrack(() => slot.output ?? (slot.button === 'find-bugs' ? 'drafts' : 'report')));
  let approval = $state<NonNullable<DeckSlot['approval']>>(untrack(() => slot.approval ?? 'changes'));
  let maxRepositories = $state(untrack(() => slot.maxRepositories ?? 0));
  let custom = $state(false);
  let busy = $state(false);
  let error = $state('');
  const definition = $derived(workflow(slot.button));
  const repositories = $derived(app.projects.filter(p => p.kind !== 'work' && !!p.repoPath));
  const project = $derived(repositories.find(p => p.id === (projectIds[0] || app.lastProjectId)));
  const inherited = $derived(resolveFor({}, project?.execution, app.globalExecution));
  const agents = $derived(executionAgents(app.agents, app.runners, projectIds[0] || app.lastProjectId, runner || inherited.runner, true));
  const options = $derived(eligibleRunners(app.runners, projectIds[0] || app.lastProjectId, runner || inherited.runner, true).flatMap(r => r.capabilities?.options ?? []).find(o => o.agentId === agent) ?? app.agentOptions.get(agent));
  const choices = $derived(modelChoices(options));
  const problem = $derived(model && !validModelName(model) ? 'Use a valid model name, without spaces or command flags.' : '');
  $effect(() => { if (agent) void app.loadAgentOptions(agent); });
  async function save(e: SubmitEvent) {
    e.preventDefault(); await submit(false);
  }
  async function submit(once: boolean) {
    if (problem || busy || (target === 'selected' && !projectIds.length)) return;
    busy = true; error = '';
    try { const next: DeckSlot = { id: slot.id, button: slot.button, target, ...(target === 'selected' ? {projectIds} : {}), agent, model: agent ? model.trim() : '', reasoning: agent ? reasoning : '', runner, output, approval, maxRepositories }; if (once && onrun) await onrun(next); else await onsave(next); }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }
</script>

<Sheet title="Configure {definition?.label ?? 'workflow'}" {onclose}>
  <form id="workflow-settings" onsubmit={save}>
    {#if onboarding}<p>By default, a tap asks which repository to use. You can also save repositories here. Agent and Model inherit your existing execution settings. Saving does not run it.</p>{/if}
    {#if !onboarding}<p>{definition?.description}</p>
    <p class="muted">Runs on Individual through your existing runner and permissions. Changes are reviewed through the task.</p>{/if}
    <label>Repository target <select class="select" bind:value={target} disabled={busy}>
      <option value="ask">Ask every time</option><option value="selected">Selected repositories</option>{#if !onboarding}<option value="current">Current project (resolved when run)</option><option value="all">All accessible repositories</option>{/if}
    </select></label>
    {#if target === 'current'}<p class="muted">Current: {app.project(app.lastProjectId)?.name ?? 'Choose a project before running'}</p>{/if}
    {#if target === 'selected'}<fieldset><legend>Repositories</legend>{#each repositories as p (p.id)}<label class="check"><input type="checkbox" bind:group={projectIds} value={p.id} disabled={busy} />{p.name}</label>{/each}</fieldset>{/if}
    {#if !onboarding}<label>Runner <select class="select" bind:value={runner} disabled={busy}><option value="">Project / controller default</option><option value="automatic">Automatic</option>{#each app.runners.filter(r => !r.disabled) as r (r.id)}<option value={r.id}>{r.name}{r.online ? '' : ' (offline)'}</option>{/each}</select></label>{/if}
    {#if !repositories.length}<p class="muted">Add a Git repository on your computer first, then return here.</p>{#if onboarding && onpreview}<button class="btn" type="button" onclick={onpreview}>Continue with interactive preview</button>{/if}{/if}
    <label>Agent <select class="select" bind:value={agent} disabled={busy} onchange={() => { model = ''; reasoning = ''; custom = false; }}>
      <option value="">Project / controller default{inherited.agent ? ` (${inherited.agent})` : ''}</option>
      {#each agents as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
      {#if agent && !agents.some(a => a.id === agent)}<option value={agent}>{agent} (unavailable)</option>{/if}
    </select></label>
    <label>Model <select class="select" disabled={busy || !agent} value={custom ? '__custom' : model} onchange={e => { if (e.currentTarget.value === '__custom') { custom = true; model = ''; } else { custom = false; model = e.currentTarget.value; } }}>
      <option value="">Inherit execution default</option>
      {#each choices.list.filter(m => m.id !== 'default') as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
      {#if model && !choices.list.some(m => m.id === model) && !custom}<option value={model}>{model}</option>{/if}
      {#if choices.custom}<option value="__custom">Another model…</option>{/if}
    </select></label>
    {#if custom && agent}<label>Model name <input class="input" bind:value={model} disabled={busy} maxlength="100" autocomplete="off" autocapitalize="off" spellcheck="false" /></label>{/if}
    {#if reasoningChoices(options, model).length > 1}<label>Reasoning <select class="select" bind:value={reasoning} disabled={busy || !agent}><option value="">Inherit execution default</option>{#each reasoningChoices(options, model) as r (r.id)}<option value={r.id}>{r.name}</option>{/each}</select></label>{/if}
    {#if !onboarding}<label>Output <select class="select" bind:value={output} disabled={busy}><option value="report">Report</option><option value="drafts">Draft tasks in Backlog</option>{#if slot.button === 'improve-tests' || slot.button === 'refactor-codebase'}<option value="changes">Propose, then implement approved changes</option>{/if}</select></label>
    <label>Approval <select class="select" bind:value={approval} disabled={busy}><option value="changes">Before changes and sensitive actions</option><option value="always">Before investigation as well</option></select></label>
    <label>Maximum repositories (0 = no additional limit) <input class="input" type="number" min="0" max="100" step="1" bind:value={maxRepositories} disabled={busy} /></label>{/if}
    {#if problem}<p class="error" role="alert">{problem}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <p class="muted">{onboarding ? 'Find Bugs saves verified findings as Backlog drafts for you to review. Full settings are available after onboarding. ' : ''}Saving configures this button. Tap it on the deck to start work. Hold it, press Shift+F10, or use Edit Deck to configure it again.</p>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={onclose} disabled={busy}>Cancel</button>
    {#if onrun}<button class="btn" type="button" disabled={busy || !!problem || (target === 'selected' && !projectIds.length)} onclick={() => submit(true)}>Run Once With Different Settings</button>{/if}
    <button class="btn primary" type="submit" form="workflow-settings" disabled={busy || (target === 'selected' && !projectIds.length) || !!problem}>{busy ? 'Saving…' : 'Save configuration'}</button>
  {/snippet}
</Sheet>
<style>
  form { display: grid; gap: 14px; }
  label { display: grid; gap: 6px; }
  p { margin: 0; }
  fieldset { border: 1px solid var(--border); border-radius: 6px; padding: 8px; }
  .check { display: flex; align-items: center; min-height: 44px; }
  .check input { width: 20px; }
  select, input { min-height: 44px; width: 100%; font-size: 16px; }
</style>
