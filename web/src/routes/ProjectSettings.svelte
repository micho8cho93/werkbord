<script lang="ts">
  import { api } from '../lib/api';
  import { compact, resolveFor, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import { app } from '../lib/state.svelte';
  import type { ExecutionConfig, Project } from '../lib/types';

  let { project }: { project: Project } = $props();

let concurrency=$state(1);let concurrencyBusy=$state(false);let concurrencyError=$state('');let concurrencySaved=$state(false);
 $effect(()=>{let active=true;api.orchestrationSettings(project.id).then(s=>{if(active)concurrency=s.concurrencyLimit;},e=>{if(active)concurrencyError=e.message;});return ()=>{active=false;};});
 async function saveConcurrency(){concurrencyBusy=true;concurrencyError='';concurrencySaved=false;try{await api.setOrchestrationSettings(project.id,concurrency);concurrencySaved=true;}catch(e){concurrencyError=e instanceof Error?e.message:String(e);}finally{concurrencyBusy=false;}}
 // This project's defaults: they override the global ones, and each task can override them in turn.
  let draft = $state<ExecutionConfig>({});
  let draftFor = '';
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  $effect(() => {
    const key = project.id + JSON.stringify(project.execution ?? {});
    if (key !== draftFor) {
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
      saved = true;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }
</script>

<div class="settings">
  <section class="card block">
    <div class="head">
      <h3>Defaults for {project.name}</h3>
      <p class="muted">
        What tasks in this project use unless the task says otherwise. Anything left on “Same as your defaults” follows your global
        defaults, so changing those changes it here too.
      </p>
    </div>
    <ExecutionFields bind:value={draft} inherited={below} idPrefix="project" />
    <p class="muted small">
      Tasks here get: <strong>{summaryLine(effective, app.agents, app.agentOptions) || 'the first agent that works, with its own defaults'}</strong>
    </p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row">
      <button class="btn primary" disabled={saving || !dirty} onclick={save}>{saving ? 'Saving…' : 'Save'}</button>
      {#if dirty}<button class="btn quiet" onclick={() => (draft = { ...(project.execution ?? {}) })}>Discard</button>{/if}
      {#if saved && !dirty}<span class="ok" role="status">Saved</span>{/if}
    </div>
  </section>

<section class="card block">
 <h3>Execution capacity</h3><label for="concurrency">Concurrent sessions in this project</label><input id="concurrency" class="input" type="number" min="1" max="16" bind:value={concurrency} />
 <p class="muted small">Running, starting, blocked, and waiting sessions all occupy capacity. Unknown or overlapping file scope still runs sequentially.</p>
 {#if concurrencyError}<p class="error" role="alert">{concurrencyError}</p>{/if}<div class="row"><button class="btn primary" disabled={concurrencyBusy} onclick={saveConcurrency}>Save capacity</button>{#if concurrencySaved}<span class="ok" role="status">Saved</span>{/if}</div>
 </section>
  <section class="card block">
    <h3>Repository</h3>
    <p class="mono path">{project.repoPath}</p>
  </section>
</div>

<style>
  .settings {
    display: grid;
    gap: 14px;
    max-width: 720px;
  }

  .block {
    display: grid;
    gap: 12px;
    padding: 14px 16px;
  }

  .head {
    display: grid;
    gap: 4px;
  }

  h3 {
    font-size: 1rem;
    font-weight: 650;
  }

  .row {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }

  .ok {
    color: var(--ok);
    font-size: 0.88rem;
  }

  .small {
    font-size: 0.82rem;
  }

  .path {
    overflow-wrap: anywhere;
    color: var(--text-2);
  }
</style>
