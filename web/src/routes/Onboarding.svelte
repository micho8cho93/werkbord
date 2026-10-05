<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import { router } from '../lib/router.svelte';
  import AgentsCard from '../lib/setup/AgentsCard.svelte';
  import GitHubCard from '../lib/setup/GitHubCard.svelte';
  import NetworkCard from '../lib/setup/NetworkCard.svelte';
  import ReposCard from '../lib/setup/ReposCard.svelte';
  import SetupCard from '../lib/setup/SetupCard.svelte';
  import { app } from '../lib/state.svelte';
  import type { GitHubStatusInfo, NetworkStatus, Runner } from '../lib/types';

  let runner = $state<Runner | null>(null);
  let github = $state<GitHubStatusInfo | null>(null);
  let net = $state<NetworkStatus | null>(null);
  let skippedNetwork = $state(false);
  let skippedGithub = $state(false);
  let finishing = $state(false);
  let error = $state('');

  onMount(() => {
    api.listRunners().then((rs) => (runner = rs[0] ?? null), () => {});
    // The cards report their own state; this page also needs them to say what was skipped.
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') api.network().then((n) => (net = n), () => {});
    }, 3000);
    api.network().then((n) => (net = n), () => {});
    void app.loadAgents();
    return () => clearInterval(t);
  });

  async function finish() {
    finishing = true;
    error = '';
    try {
      const skipped: string[] = [];
      if (net?.state !== 'connected') skipped.push('network');
      if (github?.state !== 'signed_in') skipped.push('github');
      if (!app.agents.some((a) => a.available)) skipped.push('agents');
      if (app.projects.length === 0) skipped.push('projects');
      app.onboarding = await api.completeOnboarding(skipped);
      const first = app.projects[0];
      router.go(first ? { view: 'board', projectId: first.id, taskId: '' } : { view: 'projects', projectId: '', taskId: '' });
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      finishing = false;
    }
  }

  const ready = $derived(app.agents.filter((a) => a.available).length);
  const steps = $derived([
    { label: 'This computer', done: !!runner },
    { label: 'Phone access', done: net?.state === 'connected' },
    { label: 'GitHub', done: github?.state === 'signed_in' },
    { label: 'Agents', done: ready > 0 },
    { label: 'Repositories', done: app.projects.length > 0 },
  ]);
</script>

<div class="onboarding">
  <header class="welcome">
    <h2 class="title">Welcome to Werkbord</h2>
    <p class="lede">
      A few things make it work for you. None is required to start: skip what you do not need now, and find it all again under Settings.
    </p>
    <ol class="progress" aria-label="Progress">
      {#each steps as s (s.label)}
        <li data-done={s.done}><span aria-hidden="true">{s.done ? '✓' : '○'}</span> {s.label}</li>
      {/each}
    </ol>
  </header>

  <SetupCard title="This computer" status={runner ? 'done' : 'wait'} summary={runner ? `${runner.name} · ${runner.os}/${runner.arch}` : 'Registering…'}>
    <p class="muted">
      It is ready to run coding agents: Werkbord set it up as your first runner when it was installed. Your repositories, credentials
      and sessions stay on it.
    </p>
  </SetupCard>

  <NetworkCard skipped={skippedNetwork} onskip={() => (skippedNetwork = true)} />
  <GitHubCard skipped={skippedGithub} onskip={() => (skippedGithub = true)} onchange={(s) => (github = s)} />
  <AgentsCard />
  <ReposCard {github} />

  <footer class="done">
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button class="btn primary big" disabled={finishing} onclick={finish}>
      {finishing ? 'Opening…' : app.projects.length > 0 ? 'Open Werkbord' : 'Continue without a project'}
    </button>
    <p class="muted small">You can come back to any of this under Settings.</p>
  </footer>
</div>

<style>
  .onboarding {
    display: grid;
    gap: 14px;
    max-width: 720px;
    margin: 0 auto;
  }

  .welcome {
    display: grid;
    gap: 8px;
  }

  .title {
    font-size: 1.5rem;
    text-transform: none;
    letter-spacing: 0;
    color: var(--text);
  }

  .lede {
    color: var(--text-2);
  }

  .progress {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
    display: flex;
    gap: 6px 14px;
    flex-wrap: wrap;
    font-size: 0.85rem;
    color: var(--text-2);
  }

  .progress li[data-done='true'] {
    color: var(--ok);
    font-weight: 600;
  }

  .done {
    display: grid;
    gap: 8px;
    justify-items: start;
    padding-bottom: 8px;
  }

  .big {
    min-height: 48px;
    padding: 0 22px;
  }

  .small {
    font-size: 0.82rem;
  }
</style>
