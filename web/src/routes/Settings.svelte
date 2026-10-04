<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import { compact, resolveFor, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import { router } from '../lib/router.svelte';
  import AgentsCard from '../lib/setup/AgentsCard.svelte';
  import GitHubCard from '../lib/setup/GitHubCard.svelte';
  import NetworkCard from '../lib/setup/NetworkCard.svelte';
  import SetupCard from '../lib/setup/SetupCard.svelte';
  import { app } from '../lib/state.svelte';
  import type { ExecutionConfig, Health, Runner } from '../lib/types';

  // The global defaults: the bottom of the hierarchy. A project and a task override them.
  let draft = $state<ExecutionConfig>({});
  let draftFor = '';
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');
  let runner = $state<Runner | null>(null);
  let health = $state<Health | null>(null);

  // Follows the stored defaults until the user starts editing; another device's change shows up here.
  $effect(() => {
    const key = JSON.stringify(app.globalExecution);
    if (key !== draftFor) {
      draftFor = key;
      draft = { ...app.globalExecution };
    }
  });

  onMount(() => {
    api.listRunners().then((rs) => (runner = rs[0] ?? null), () => {});
    api.health().then((h) => (health = h), () => {});
  });

  const dirty = $derived(JSON.stringify(compact(draft)) !== JSON.stringify(compact(app.globalExecution)));
  const effective = $derived(resolveFor({}, {}, compact(draft)));

  async function save() {
    saving = true;
    error = '';
    saved = false;
    try {
      await app.saveGlobalExecution(compact(draft));
      saved = true;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  async function runSetupAgain() {
    app.onboarding = await api.resetOnboarding().catch(() => app.onboarding);
    router.go({ view: 'onboarding', projectId: '', taskId: '' });
  }
</script>

<div class="settings">
  <section class="card block">
    <div class="head">
      <h3>Defaults for every task</h3>
      <p class="muted">
        What a task uses unless its project or the task itself says otherwise. You do not have to set any of it: left alone, the agent
        picks its own model and reasoning.
      </p>
    </div>
    <ExecutionFields bind:value={draft} idPrefix="global" />
    {#if app.agents.length > 0}
      <p class="muted small">
        New tasks get: <strong>{summaryLine(effective, app.agents, app.agentOptions) || 'the first agent that works, with its own defaults'}</strong>
      </p>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row">
      <button class="btn primary" disabled={saving || !dirty} onclick={save}>{saving ? 'Saving…' : 'Save defaults'}</button>
      {#if dirty}<button class="btn quiet" onclick={() => (draft = { ...app.globalExecution })}>Discard</button>{/if}
      {#if saved && !dirty}<span class="ok" role="status">Saved</span>{/if}
    </div>
  </section>

  <NetworkCard />
  <GitHubCard />
  <AgentsCard />

  <SetupCard title="This computer" status={runner ? 'done' : 'todo'} summary={runner ? `${runner.name} · ${runner.os}/${runner.arch}` : ''}>
    {#if runner}
      <p class="muted">It is registered as your runner and {runner.online ? 'online' : 'offline'}. Agents run here, in Git worktrees, as you.</p>
    {/if}
    {#if health}<p class="muted small">Dev Board {health.version} · database {health.database}. Run <code>devboard doctor</code> in a terminal to check everything.</p>{/if}
    <div class="row"><button class="btn small" onclick={runSetupAgain}>Run setup again</button></div>
  </SetupCard>
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
</style>
