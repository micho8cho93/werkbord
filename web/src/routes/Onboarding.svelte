<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import { globalHref, router } from '../lib/router.svelte';
  import AgentsCard from '../lib/setup/AgentsCard.svelte';
  import GitHubCard from '../lib/setup/GitHubCard.svelte';
  import NetworkCard from '../lib/setup/NetworkCard.svelte';
  import ReposCard from '../lib/setup/ReposCard.svelte';
  import SetupCard from '../lib/setup/SetupCard.svelte';
  import { localReadiness } from '../lib/setup/readiness';
  import { app } from '../lib/state.svelte';
  import type { GitHubStatusInfo, NetworkStatus } from '../lib/types';

  let github = $state<GitHubStatusInfo | null>(null);
  let net = $state<NetworkStatus | null>(null);
  let skippedNetwork = $state(false);
  let skippedGithub = $state(false);
  let finishing = $state(false);
  let error = $state('');
  let readinessUnavailable = $state(false);
  let checking = $state(false);
  let computerSection: HTMLDivElement | undefined = $state();
  let agentsSection: HTMLDivElement | undefined = $state();
  let repositorySection: HTMLDivElement | undefined = $state();
  const local = $derived(localReadiness(app.runners, app.agents, app.projects, app.connection === 'live' && !readinessUnavailable));

  async function recheck() {
    checking = true;
    error = '';
    try {
      const [runners, agents] = await Promise.all([api.listRunners(), api.listAgents()]);
      app.runners = runners;
      app.agents = agents;
      readinessUnavailable = false;
    } catch (err) {
      readinessUnavailable = true;
      error = err instanceof Error ? err.message : String(err);
    } finally { checking = false; }
  }

  function showNext() {
    const section = local.next === 'computer' ? computerSection : local.next === 'agents' ? agentsSection : repositorySection;
    section?.scrollIntoView({ block: 'start' });
    section?.focus({ preventScroll: true });
    if (local.next !== 'repository') void recheck();
  }

  onMount(() => {
    void recheck();
    // The cards report their own state; this page also needs them to say what was skipped.
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') api.network().then((n) => (net = n), () => {});
    }, 3000);
    api.network().then((n) => (net = n), () => {});
    return () => clearInterval(t);
  });

  async function finish(createTask = false) {
    if (createTask && !local.ready) return;
    finishing = true;
    error = '';
    try {
      const skipped: string[] = [];
      if (net?.state !== 'connected') skipped.push('network');
      if (github?.state !== 'signed_in') skipped.push('github');
      if (!app.agents.some((a) => a.available)) skipped.push('agents');
      if (!local.project) skipped.push('projects');
      app.onboarding = await api.completeOnboarding(skipped);
      const first = createTask ? local.project : app.projects[0];
      router.go(first ? { view: 'board', projectId: first.id, taskId: '' } : { view: 'projects', projectId: '', taskId: '' });
      if (createTask) app.newTaskOpen = true;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      finishing = false;
    }
  }

  const nextLabel = $derived(local.next === 'computer' ? 'Check local runner' : local.next === 'agents' ? 'Set up a coding agent' : 'Add a local repository');
</script>

<div class="onboarding">
  <header class="welcome">
    <h2 class="title">Welcome to Werkbord</h2>
    <p class="lede">
      Run your first local coding task. Your runner, agent sign-in and repository stay on your computer. You can also continue with planning only.
    </p>
    <p class="progress-label" role="status">{local.completed} of 3 requirements verified{local.ready ? ' · ready to create a task' : ''}</p>
    <ol class="progress" aria-label="Progress">
      {#each local.steps as s (s.id)}
        <li class="chip" data-done={s.done}><span class="dot" data-tone={s.done ? 'ok' : 'neutral'}></span>{s.label}<span class="visually-hidden">{s.done ? ', done' : ', to do'}</span></li>
      {/each}
    </ol>
    <div class="actions">
      {#if local.ready}<button class="btn primary big" disabled={checking || finishing} onclick={() => finish(true)}>{finishing ? 'Opening…' : 'Create your first task'}</button>
      {:else}<button class="btn primary big" disabled={checking || finishing} onclick={showNext}>{checking ? 'Checking…' : nextLabel}</button>{/if}
      <button class="btn big" disabled={finishing} onclick={() => finish(false)}>Continue with planning only</button>
    </div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </header>

  <div bind:this={computerSection} tabindex="-1" class="requirement">
  <SetupCard title="This computer" status={local.steps[0].done ? 'done' : checking ? 'wait' : 'todo'} summary={local.runner ? `${local.runner.name} · ${local.runner.os}/${local.runner.arch}` : 'No local runner reported'}>
    <p class="muted">
      {local.steps[0].done ? 'The local runner is online and enabled.' : app.connection !== 'live' ? 'Reconnect to verify local runner readiness.' : 'The local runner must be online, enabled and configured to run an agent.'} Your repositories, credentials and sessions stay on this computer.
    </p>
    {#if !local.steps[0].done}<p><a href="{globalHref('settings')}?runners">Open runner settings</a> to inspect or enable your local runner.</p>{/if}
    {#if local.runner && local.runner.currentRuns >= local.runner.capacity && local.runner.capacity > 0}<p class="muted">The runner is at capacity. You can create a task now and start it when capacity is available.</p>{/if}
  </SetupCard>
  </div>

  <div bind:this={agentsSection} tabindex="-1" class="requirement">
  <AgentsCard />
  </div>
  <div bind:this={repositorySection} tabindex="-1" class="requirement">
  <ReposCard {github} />
  </div>

  <details class="optional">
    <summary>Optional connections <span class="muted">Phone access and GitHub</span></summary>
    <div class="optional-body">
      <p class="muted">Neither is required to run a local coding task. Configure them here or later in Settings.</p>
      <p class="optional-status">Phone access: {net?.state === 'connected' ? 'Connected' : skippedNetwork ? 'Optional—skipped' : 'Optional'} · GitHub: {github?.state === 'signed_in' ? 'Connected' : skippedGithub ? 'Optional—skipped' : 'Optional'}</p>
      <NetworkCard skipped={skippedNetwork} onskip={() => (skippedNetwork = true)} />
      <GitHubCard skipped={skippedGithub} onskip={() => (skippedGithub = true)} onchange={(s) => (github = s)} />
    </div>
  </details>

  <footer class="done">
    <p class="muted small">You can come back to any of this under Settings.</p>
  </footer>
</div>

<style>
  .onboarding {
    display: grid;
    gap: 16px;
    max-width: 760px;
    margin: 0 auto;
    padding-top: 12px;
  }

  .welcome {
    display: grid;
    gap: 8px;
  }

  .title {
    font-family: var(--font);
    font-size: 28px;
    font-weight: 600;
    text-transform: none;
    letter-spacing: -0.02em;
    color: var(--text);
  }

  .lede {
    color: var(--text-2);
  }

  .progress {
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
    display: flex;
    gap: 6px 8px;
    flex-wrap: wrap;
  }
  .progress-label { font-size: 13px; font-weight: 500; }
  .requirement { scroll-margin-top: 16px; outline-offset: 3px; }
  .optional { border-top: 1px solid var(--border); padding-top: 12px; }
  .optional summary { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; min-height: 44px; cursor: pointer; font-weight: 500; }
  .optional-body { display: grid; gap: 16px; padding-top: 12px; }
  .optional-status { font-size: 12px; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }

  .progress li[data-done='true'] {
    color: var(--text);
  }

  .progress .dot[data-tone='neutral'] {
    opacity: 0.5;
  }

  .done {
    display: grid;
    gap: 8px;
    justify-items: start;
    padding-bottom: 8px;
  }

  .big {
    min-height: 44px;
    padding: 0 20px;
    font-size: 14px;
  }

  .small {
    font-size: 0.82rem;
  }
</style>
