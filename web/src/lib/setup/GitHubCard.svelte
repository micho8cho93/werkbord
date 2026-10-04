<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import { copyText } from '../clipboard';
  import type { GitHubStatusInfo } from '../types';
  import SetupCard from './SetupCard.svelte';

  let {
    skipped = false,
    onskip,
    onchange,
  }: { skipped?: boolean; onskip?: () => void; onchange?: (s: GitHubStatusInfo) => void } = $props();

  let gh = $state<GitHubStatusInfo | null>(null);
  let busy = $state(false);
  let error = $state('');
  let copied = $state(false);

  async function refresh() {
    try {
      const was = gh?.state;
      gh = await api.github();
      error = '';
      if (was !== gh.state) onchange?.(gh);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  onMount(() => {
    void refresh();
    // While the user approves on github.com the state changes by itself.
    const t = setInterval(() => {
      if (document.visibilityState === 'visible' && (gh?.state === 'signing_in' || gh?.state === 'signed_out')) void refresh();
    }, 2000);
    return () => clearInterval(t);
  });

  async function connect() {
    busy = true;
    error = '';
    try {
      await api.githubLogin();
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function cancel() {
    gh = await api.githubCancelLogin().catch(() => gh);
  }

  async function copyCode() {
    if (gh?.login.code && (await copyText(gh.login.code))) {
      copied = true;
      setTimeout(() => (copied = false), 1800);
    }
  }

  const status = $derived(
    gh?.state === 'signed_in' ? 'done' : skipped || gh?.state === 'disabled' ? 'off' : gh?.state === 'signing_in' ? 'wait' : 'todo',
  );
  const summary = $derived(
    !gh
      ? ''
      : gh.state === 'signed_in'
        ? `Connected as ${gh.account?.login ?? ''}`
        : gh.state === 'disabled'
          ? 'Turned off in config.json'
          : skipped
            ? 'Skipped'
            : 'Optional',
  );
</script>

<SetupCard title="GitHub" {status} {summary}>
  {#if !gh}
    <p class="muted">Checking…</p>
  {:else if gh.state === 'signed_in'}
    <p>
      Dev Board can list your repositories and show pull requests and checks, using your own GitHub sign-in (through the GitHub CLI).
      Nothing about Dev Board is stored in GitHub.
    </p>
    <p class="muted small">To disconnect, run <code>gh auth logout</code> in a terminal.</p>
  {:else if gh.state === 'signing_in'}
    <p>On GitHub, enter this code and approve:</p>
    <p class="code"><code class="mono">{gh.login.code}</code></p>
    <div class="row">
      <a class="btn primary" href={gh.login.url ?? 'https://github.com/login/device'} target="_blank" rel="noopener noreferrer">Open GitHub</a>
      <button class="btn" onclick={copyCode}>{copied ? 'Copied' : 'Copy code'}</button>
      <button class="btn quiet" onclick={cancel}>Cancel</button>
    </div>
    <p class="muted small">This page notices when you are done.</p>
  {:else if gh.state === 'missing'}
    <p>Connecting GitHub uses the GitHub CLI, which is not installed.</p>
    {#if gh.guidance}<p class="muted">{gh.guidance}</p>{/if}
    <div class="row">
      <button class="btn" onclick={refresh}>Check again</button>
      {#if onskip && !skipped}<button class="btn quiet" onclick={onskip}>Skip for now</button>{/if}
    </div>
  {:else if gh.state === 'disabled'}
    <p class="muted">{gh.message}</p>
  {:else if gh.state === 'error'}
    <p class="error" role="alert">{gh.message}</p>
    <div class="row"><button class="btn" onclick={refresh}>Try again</button></div>
  {:else}
    <p>
      Connect your GitHub account to pick repositories, and to see branches, pull requests and checks. Optional: everything local works
      without it, and private repositories are fine.
    </p>
    {#if gh.login.state === 'failed'}<p class="error" role="alert">{gh.login.error}</p>{/if}
    <div class="row">
      <button class="btn primary" disabled={busy} onclick={connect}>{busy ? 'Starting…' : 'Connect GitHub'}</button>
      {#if onskip && !skipped}<button class="btn quiet" onclick={onskip}>Skip for now</button>{/if}
    </div>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</SetupCard>

<style>
  .row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .code {
    font-size: 1.6rem;
    letter-spacing: 0.08em;
  }

  .code code {
    font-size: inherit;
  }

  .small {
    font-size: 0.8rem;
  }
</style>
