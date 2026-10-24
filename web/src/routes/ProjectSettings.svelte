<script lang="ts">
  import { isPhone } from '../lib/phones/capability';
  import { untrack } from 'svelte';
  import { api } from '../lib/api';
  import { compact, resolveFor, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import Icon from '../lib/Icon.svelte';
  import ProjectRelationships from '../lib/ProjectRelationships.svelte';
  import ProjectLifecycle from '../lib/ProjectLifecycle.svelte';
  import ProjectMemory from '../lib/ProjectMemory.svelte';
  import { app } from '../lib/state.svelte';
  import type { ExecutionConfig, Project } from '../lib/types';

  let { project }: { project: Project } = $props();

  let concurrency = $state(1);
  let savedConcurrency = $state<number | null>(null);
  let concurrencyBusy = $state(false);
  let concurrencyError = $state('');
  let concurrencySaved = $state(false);
  const capacityChanged = $derived(savedConcurrency !== null && concurrency !== savedConcurrency);
  const capacityValid = $derived(Number.isInteger(concurrency) && concurrency >= 1 && concurrency <= 16);
  $effect(() => {
    let active = true;
    savedConcurrency = null;
    concurrencySaved = false;
    concurrencyError = '';
    api.orchestrationSettings(project.id).then(settings => {
      if (active) { concurrency = settings.concurrencyLimit; savedConcurrency = settings.concurrencyLimit; }
    }, error => { if (active) concurrencyError = error.message; });
    return () => { active = false; };
  });
  async function saveConcurrency() {
    if (concurrencyBusy || !capacityChanged || !capacityValid) return;
    const projectId = project.id;
    concurrencyBusy = true;
    concurrencyError = '';
    concurrencySaved = false;
    try {
      const saved = await api.setOrchestrationSettings(projectId, concurrency);
      if (project.id === projectId) { savedConcurrency = saved.concurrencyLimit; concurrencySaved = true; }
    } catch (error) {
      if (project.id === projectId) concurrencyError = error instanceof Error ? error.message : String(error);
    } finally { concurrencyBusy = false; }
  }
  // This project's defaults: they override the global ones, and each task can override them in turn.
  let draft = $state<ExecutionConfig>({});
  let draftFor = '';
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  $effect(() => {
    const key = project.id + JSON.stringify(project.execution ?? {});
    if (key !== draftFor && untrack(() => !draftFor || !draftFor.startsWith(project.id) || project.id + JSON.stringify(draft) === draftFor)) {
      draftFor = key;
      draft = { ...(project.execution ?? {}) };
    }
  });

  const dirty = $derived(JSON.stringify(compact(draft)) !== JSON.stringify(compact(project.execution ?? {})));
  /** What a task here gets if it sets nothing itself. */
  const below = $derived(resolveFor({}, {}, app.globalExecution));
  const effective = $derived(resolveFor({}, compact(draft), app.globalExecution));

  async function save() {
    saving = true;
    error = '';
    saved = false;
    try {
      app.upsertProject(await api.setProjectExecution(project.id, compact(draft)));
      draftFor = project.id + JSON.stringify(compact(draft));
      saved = true;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }
  let section = $state<'defaults' | 'capacity' | 'repository' | 'memory' | 'relationships' | 'history'>('defaults');
</script>

{#if isPhone()}
  <section class="pn"><h3>{project.name}</h3><p class="muted">A project's settings are changed at your computer.</p></section>
{:else if project.kind === 'work'}
  <div class="settings single">
    <section class="pn">
      <h3>{project.name}</h3>
      <p class="muted">
        A work project is a board and a timeline for anything that is not code. It has no repository, so there are no agents, runs or task
        defaults to set. Planning dates are set on each task.
      </p>
      <dl class="kv"><dt>Project ID</dt><dd class="mono">{project.id}</dd></dl>
    </section>
    <ProjectMemory projectId={project.id} />
    <ProjectLifecycle {project} />
  </div>
{:else}
<div class="settings">
  <nav class="sections" aria-label="Project settings sections">
    <button class="btn" aria-pressed={section === 'defaults'} onclick={() => section = 'defaults'}><Icon name="defaults" />Task defaults</button>
    <button class="btn" aria-pressed={section === 'capacity'} onclick={() => section = 'capacity'}><Icon name="runner" />Capacity</button>
    <button class="btn" aria-pressed={section === 'repository'} onclick={() => section = 'repository'}><Icon name="git" />Repository</button>
    <button class="btn" aria-pressed={section === 'relationships'} onclick={() => section = 'relationships'}>Relationships</button>
    <button class="btn" aria-pressed={section === 'history'} onclick={() => section = 'history'}>Project history</button>
    <button class="btn" aria-pressed={section === 'memory'} onclick={() => section = 'memory'}>Memory</button>
  </nav>
  <div class="content">
  {#if section === 'history'}
    <ProjectLifecycle {project} />
  {:else if section === 'relationships'}
    <ProjectRelationships projectId={project.id} />
  {:else if section === 'memory'}
    <ProjectMemory projectId={project.id} />
  {:else if section === 'defaults'}
  <section class="pn">
    <div class="head">
      <h3>Defaults for {project.name}</h3>
      <p class="muted">
        What tasks in this project use unless the task says otherwise. Anything left on “Same as your defaults” follows your global
        defaults, so changing those changes it here too.
      </p>
    </div>
    <ExecutionFields bind:value={draft} inherited={below} idPrefix="project" columns />
    <p class="muted small">
      Tasks here get: <strong>{summaryLine(effective, app.agents, app.agentOptions) || 'Lowest cost, selected when the run starts'}</strong>
    </p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row">
      <button class="btn primary" disabled={saving || !dirty} onclick={save}>{saving ? 'Saving…' : 'Save'}</button>
      {#if dirty}<button class="btn quiet" onclick={() => (draft = { ...(project.execution ?? {}) })}>Discard</button>{/if}
      {#if saved && !dirty}<span class="ok" role="status">Saved</span>{/if}
    </div>
  </section>

{:else if section === 'capacity'}
<section class="pn">
 <h3>Concurrent runs</h3><label for="concurrency">Concurrent sessions in this project</label><input id="concurrency" class="input narrow" type="number" min="1" max="16" bind:value={concurrency} disabled={concurrencyBusy || savedConcurrency === null} />
 <p class="muted small">Running, starting, blocked, and waiting sessions all occupy capacity. Unknown or overlapping file scope still runs sequentially.</p>
 {#if concurrencyError}<p class="error" role="alert">{concurrencyError}</p>{/if}<div class="row"><button class="btn primary" disabled={concurrencyBusy || !capacityChanged || !capacityValid} onclick={saveConcurrency}>Save capacity</button>{#if concurrencySaved && concurrency === savedConcurrency}<span class="ok" role="status">Saved</span>{/if}</div>
 </section>
  {:else}
  <section class="pn">
    <h3>Repository</h3>
    <dl class="kv">
      <dt>Path</dt><dd class="mono">{project.repoPath}</dd>
      {#if project.repository}
        <dt>Branch</dt><dd class="mono">{project.repository.currentBranch || 'detached'}</dd>
        <dt>Default</dt><dd class="mono">{project.repository.defaultBranch || '—'}</dd>
        <dt>Remotes</dt><dd class="mono">{project.repository.remotes.map((r) => r.name).join(', ') || 'none'}</dd>
      {/if}
      <dt>Project ID</dt><dd class="mono">{project.id}</dd>
    </dl>
  </section>
  {/if}
  </div>
</div>
{/if}

<style>
  .settings {
    display: grid;
    gap: 16px;
    grid-template-columns: 200px minmax(0, 1fr);
    width: 100%;
  }

  .single { grid-template-columns: minmax(0, 1fr); max-width: 720px; }
  .sections { display: flex; flex-direction: column; align-items: stretch; gap: 8px; }
  .sections button { justify-content: start; }
  .sections button[aria-pressed='true'] { color: var(--accent); box-shadow: var(--press-sh); }
  .content { min-width: 0; max-width: 1200px; }
  @media (max-width: 899px) {
    .settings { grid-template-columns: minmax(0, 1fr); }
    .sections { flex-direction: row; flex-wrap: wrap; }
  }
  .head {
    display: grid;
    gap: 4px;
  }

  h3 {
    font-size: 15px;
  }

  label {
    font-size: 13px;
    font-weight: 500;
  }

  .narrow {
    max-width: 8rem;
  }

  .row {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-wrap: wrap;
  }

  .ok {
    color: var(--ok-text);
    font-size: 13px;
  }

  .small {
    font-size: 13px;
  }

  .kv {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 6px 16px;
    margin: 0;
    font-size: 13px;
  }

  .kv dt {
    color: var(--text-2);
  }

  .kv dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
</style>
