<script lang="ts">
  import {
    AGENT_DEFAULT,
    executionAgents,
    eligibleRunners,
    PRIORITY_OPTIONS,
    agentLabel,
    compact,
    modelChoices,
    optionLabel,
    priorityLabel,
    reasoningChoices,
    sourceLabel,
    validModelName,
    type Resolved,
  } from './execution';
  import { INTERACTION_OPTIONS, PERMISSIONS_NOTE, interactionLabel } from './policy';
  import { router } from './router.svelte';
  import { app } from './state.svelte';
  import type { ExecutionConfig, InteractionPolicy, Priority } from './types';

  /**
   * How work is carried out, at one level: global defaults, a project, a task, or one run. A field
   * left on its first option ("Same as …") is not set here, and the level below decides. Model and
   * reasoning belong to an agent, so choosing either also fixes the agent.
   */
  let {
    value = $bindable<ExecutionConfig>({}),
    inherited,
    idPrefix,
    showPriority = true,
    disabled = false,
    dense = false,
  }: {
    value?: ExecutionConfig;
    /** What applies if this level sets nothing: the levels below it, resolved. Without it there is no level below. */
    inherited?: Resolved;
    idPrefix: string;
    showPriority?: boolean;
    disabled?: boolean;
    /** Two columns of bare pickers, no explanations: for a dialog or a side panel where everything shows at once. */
    dense?: boolean;
  } = $props();

  const hasBelow = $derived(inherited !== undefined);
  // The agent whose models and reasoning levels are offered: this level's own, else the one it would inherit.
  const agentForOptions = $derived(value.agent || inherited?.agent || '');
  const environment = $derived(eligibleRunners(app.runners, router.projectId, value.runner || inherited?.runner, true));
  const options = $derived(environment.find(r => r.capabilities?.agents.some(a => a.id === agentForOptions && a.available))?.capabilities?.options.find(o => o.agentId === agentForOptions) ?? (agentForOptions ? app.agentOptions.get(agentForOptions) : undefined));
  const models = $derived(modelChoices(options));
  const effectiveModel = $derived(value.model && value.model !== AGENT_DEFAULT ? value.model : (inherited?.model ?? ''));
  const reasoningList = $derived(reasoningChoices(options, effectiveModel));

  $effect(() => {
    if (agentForOptions) void app.loadAgentOptions(agentForOptions);
  });

  // The model picker shows a typed name as "Other…" with the name in a box.
  let customOpen = $state(false);
  let customName = $state('');
  $effect(() => {
    const m = value.model ?? '';
    const known = !m || m === AGENT_DEFAULT || models.list.some((o) => o.id === m);
    if (!known) {
      customOpen = true;
      customName = m;
    }
  });
  const customProblem = $derived(customOpen && customName.trim() !== '' && !validModelName(customName.trim()) ? 'That does not look like a model name.' : '');

  const usable = $derived(executionAgents(app.agents, app.runners, router.projectId, value.runner || inherited?.runner, true));
  const unavailable = $derived(app.agents.filter((a) => !usable.some(x=>x.id===a.id) && a.id === value.agent));

  // Narrow screens keep the model and reasoning behind a disclosure.
  const wide = typeof matchMedia === 'function' ? matchMedia('(min-width: 700px)').matches : true;
  const hasAdvanced = $derived(!!(value.model || value.reasoning));

  function set(patch: Partial<ExecutionConfig>) {
    value = compact({ ...value, ...patch });
  }

  function chooseAgent(e: Event) {
    const agent = (e.currentTarget as HTMLSelectElement).value;
    // A model or reasoning level means nothing to another agent.
    customOpen = false;
    customName = '';
    value = compact({ ...value, agent, model: '', reasoning: '' });
  }

  function chooseModel(e: Event) {
    const v = (e.currentTarget as HTMLSelectElement).value;
    if (v === '__custom') {
      customOpen = true;
      return;
    }
    customOpen = false;
    customName = '';
    // Choosing a model fixes the agent: a model belongs to one.
    set({ agent: value.agent || inherited?.agent || '', model: v, reasoning: v ? value.reasoning : '' });
  }

  function typeModel(e: Event) {
    customName = (e.currentTarget as HTMLInputElement).value;
    const name = customName.trim();
    if (name && validModelName(name)) set({ agent: value.agent || inherited?.agent || '', model: name });
  }

  function chooseReasoning(e: Event) {
    set({ agent: value.agent || inherited?.agent || '', reasoning: (e.currentTarget as HTMLSelectElement).value });
  }

  const chooseInteraction = (e: Event) => set({ interaction: ((e.currentTarget as HTMLSelectElement).value || undefined) as InteractionPolicy | undefined });
  const choosePriority = (e: Event) => set({ priority: ((e.currentTarget as HTMLSelectElement).value || undefined) as Priority | undefined });

  /** "Same as the project (Claude Code)": what leaving a field alone means. */
  function sameAs(what: string, source: Resolved['sources'][keyof Resolved['sources']]): string {
    return `Same as ${source === 'default' ? 'the default' : sourceLabel(source)} (${what})`;
  }

  const interactionNow = $derived(INTERACTION_OPTIONS.find((o) => o.value === (value.interaction ?? inherited?.interaction ?? 'interactive')));
  const noAgentYet = $derived(!agentForOptions);
</script>

<div class="fields" class:compact={dense}>
  <div class="field">
    <label for="{idPrefix}-runner">Runner</label>
    <select id="{idPrefix}-runner" class="select" {disabled} value={value.runner ?? ''} onchange={e=>set({runner:e.currentTarget.value})}>
      <option value="">{hasBelow ? 'Same as project / defaults' : 'Automatic'}</option>
      {#if hasBelow}<option value="automatic">Automatic</option>{/if}
      {#each app.runners as r (r.id)}<option value={r.id}>{r.name} · {r.disabled ? 'Disabled' : r.online ? `${r.currentRuns}/${r.capacity} capacity` : 'Offline'}</option>{/each}
      {#if value.runner && value.runner !== 'automatic' && !app.runners.some(r=>r.id===value.runner)}<option value={value.runner}>Unavailable runner ({value.runner})</option>{/if}
    </select>
    <p class="hint">A specific runner keeps the task queued when unavailable. Automatic uses eligible machines with automatic routing enabled.</p>
  </div>
  <div class="field">
    <label for="{idPrefix}-agent">Agent</label>
    <select id="{idPrefix}-agent" class="select" {disabled} value={value.agent ?? ''} onchange={chooseAgent}>
      <option value="">
        {#if hasBelow && inherited?.agent}{sameAs(agentLabel(app.agents, inherited.agent), inherited.sources.agent)}{:else}Automatic: the first agent that works{/if}
      </option>
      {#each usable as a (a.id)}
        <option value={a.id}>{a.name}{a.version ? ` ${a.version}` : ''}</option>
      {/each}
      {#each unavailable as a (a.id)}
        <option value={a.id}>{a.name} (not available now)</option>
      {/each}
    </select>
    {#if usable.length === 0}
      <p class="hint keep">No coding agent is available on an online runner yet. Check Settings → Agents and Runners.</p>
    {/if}
  </div>

  <div class="field">
    <label for="{idPrefix}-interaction">Interaction</label>
    <select id="{idPrefix}-interaction" class="select" {disabled} value={value.interaction ?? ''} onchange={chooseInteraction}>
      {#if hasBelow}
        <option value="">{sameAs(interactionLabel({ interaction: inherited?.interaction ?? 'interactive' }), inherited?.sources.interaction ?? 'default')}</option>
      {:else}
        <option value="">Ask me when needed (default)</option>
      {/if}
      {#each INTERACTION_OPTIONS as o (o.value)}
        <option value={o.value}>{o.label}</option>
      {/each}
    </select>
    {#if value.interaction || !hasBelow}
      <p class="hint">{interactionNow?.description} {PERMISSIONS_NOTE}</p>
    {/if}
  </div>

  {#if showPriority}
    <div class="field">
      <label for="{idPrefix}-priority">Priority</label>
      <select id="{idPrefix}-priority" class="select" {disabled} value={value.priority ?? ''} onchange={choosePriority}>
        {#if hasBelow}
          <option value="">{sameAs(priorityLabel(inherited?.priority), inherited?.sources.priority ?? 'default')}</option>
        {:else}
          <option value="">Normal (default)</option>
        {/if}
        {#each PRIORITY_OPTIONS as o (o.value)}
          <option value={o.value}>{o.label}</option>
        {/each}
      </select>
    </div>
  {/if}

  {#snippet modelFields()}
    {#if noAgentYet}
      <p class="hint">Choose an agent first: models and reasoning levels belong to an agent.</p>
    {:else}
      <div class="field">
        <label for="{idPrefix}-model">Model</label>
        <select
          id="{idPrefix}-model"
          class="select"
          {disabled}
          value={customOpen ? '__custom' : (value.model ?? '')}
          onchange={chooseModel}
        >
          {#if hasBelow}
            <option value="">{sameAs(inherited?.model ? optionLabel(options?.models, inherited.model) : 'Agent default', inherited?.sources.model ?? 'default')}</option>
          {:else}
            <option value="">Agent default</option>
          {/if}
          {#each models.list as o (o.id)}
            <option value={o.id}>{o.name}{o.default && o.id !== AGENT_DEFAULT ? ' (its default)' : ''}</option>
          {/each}
          {#if models.custom}<option value="__custom">Other…</option>{/if}
        </select>
        {#if customOpen}
          <input
            class="input mono"
            aria-label="Model name"
            placeholder="Model name, exactly as the agent knows it"
            autocapitalize="off"
            autocomplete="off"
            spellcheck="false"
            {disabled}
            value={customName}
            oninput={typeModel}
          />
          {#if customProblem}<p class="error" role="alert">{customProblem}</p>{/if}
        {/if}
        {#if options?.note}<p class="hint">{options.note}</p>{/if}
        {#if options && options.modelsSource === 'builtin' && !options.note}
          <p class="hint">{agentLabel(app.agents, agentForOptions)} cannot list its models, so these are its stable aliases. Pick “Other…” for any model name.</p>
        {/if}
      </div>

      <div class="field">
        <label for="{idPrefix}-reasoning">Reasoning</label>
        <select id="{idPrefix}-reasoning" class="select" {disabled} value={value.reasoning ?? ''} onchange={chooseReasoning}>
          {#if hasBelow}
            <option value="">{sameAs(inherited?.reasoning ? optionLabel(options?.reasoning, inherited.reasoning) : 'Agent default', inherited?.sources.reasoning ?? 'default')}</option>
          {:else}
            <option value="">Agent default</option>
          {/if}
          {#each reasoningList as o (o.id)}
            <option value={o.id}>{o.name}</option>
          {/each}
        </select>
        {#if reasoningList.length <= 1}<p class="hint">This agent does not list reasoning levels, so only its default is offered.</p>{/if}
      </div>
    {/if}
  {/snippet}

  {#if dense}
    {@render modelFields()}
  {:else}
  <details class="advanced" open={wide || hasAdvanced}>
    <summary>
      Model and reasoning
      {#if value.model || value.reasoning}
        <span class="chosen">· {[value.model && optionLabel(options?.models, value.model), value.reasoning && optionLabel(options?.reasoning, value.reasoning)].filter(Boolean).join(' · ')}</span>
      {/if}
    </summary>

    {@render modelFields()}
  </details>
  {/if}
</div>

<style>
  .fields {
    display: grid;
 grid-template-columns:minmax(0,1fr);
    gap: 12px;
  }

  .field {
    display: grid;
 grid-template-columns:minmax(0,1fr);min-width:0;
    gap: 4px;
  }

  label {
    font-size: 13px;
    font-weight: 500;
  }

  .hint {
    font-size: 12px;
    color: var(--text-2);
  }

  .advanced {
    display: grid;
    gap: 12px;
  }

  summary {
    min-height: 40px;
    display: flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
    font-size: 0.9rem;
    font-weight: 500;
  }

  .chosen {
    font-weight: 450;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .advanced > :global(.field) {
    margin-top: 8px;
  }

  .compact {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px 12px;
  }

  .compact label {
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 500;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  .compact .hint:not(.keep) {
    display: none;
  }

  .compact .hint.keep {
    grid-column: 1 / -1;
  }

  @media (max-width: 520px) {
    .compact {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
