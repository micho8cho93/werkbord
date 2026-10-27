<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { api } from '../api';
  import { copyText } from '../clipboard';
  import type { GitHubStatusInfo } from '../types';
  import Settling from '../Settling.svelte';
  import SetupCard from './SetupCard.svelte';

  let {
    skipped = false,
    known = null,
    onskip,
    onchange,
  }: { skipped?: boolean; /** What setup already found out, so the card does not ask again and say "Checking…" for an answer in hand. */ known?: GitHubStatusInfo | null; onskip?: () => void; onchange?: (s: GitHubStatusInfo) => void } = $props();

  let gh = $state<GitHubStatusInfo | null>(untrack(() => known));
  let busy = $state(false);
  let error = $state('');
  /** Looking up the connection failed. GitHub is optional, so this is a quiet line with a retry, never a red alert on first run. */
  let unreachable = $state('');
  let copied = $state(false);
  let missingKey = '';

  async function refresh() {
    try {
      const was = gh?.state;
      gh = await api.github();
      unreachable = '';
      if (was !== gh.state || missingKey !== (gh.missingScopes ?? []).join()) onchange?.(gh);
      missingKey = (gh.missingScopes ?? []).join();
    } catch (err) {
      unreachable = err instanceof Error ? err.message : String(err);
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

  async function grant() {
    busy = true;
    error = '';
    try {
      await api.githubScopes();
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
    gh?.state === 'signed_in' ? (gh.missingScopes?.length ? 'todo' : 'done') : skipped || gh?.state === 'disabled' ? 'off' : gh?.state === 'signing_in' ? 'wait' : 'todo',
  );
  const summary = $derived(
    !gh
      ? ''
      : gh.state === 'signed_in'
        ? `Connected as ${gh.account?.login ?? ''}${gh.missingScopes?.length ? ' · needs more permission' : ''}`
        : gh.state === 'disabled'
          ? 'Turned off in config.json'
          : skipped
            ? 'Optional—skipped'
            : 'Optional',
  );
</script>

<SetupCard title="GitHub" {status} {summary}>
  {#if !gh && unreachable}
    <p class="muted" role="status">GitHub could not be checked just now ({unreachable}). It is optional: everything local works without it.</p>
    <div class="row"><button class="btn quiet" onclick={refresh}>Try again</button></div>
  {:else if !gh}
    <Settling compact label="Checking GitHub…" testid="github-loading" />
  {:else if gh.state === 'signed_in'}
    <p>Your connection uses the GitHub CLI on this computer.</p>
    {#if gh.missingScopes?.length}
      <p>
        To create repositories and publish a page, GitHub also needs to allow <strong>{gh.missingScopes.join(' and ')}</strong>.
        You approve it on github.com with a one-time code, as you did when connecting.
      </p>
      <div class="row"><button class="btn primary" disabled={busy} onclick={grant}>{busy ? 'Starting…' : 'Allow on GitHub'}</button></div>
    {/if}
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
    <div class="row"><button class="btn quiet" onclick={refresh}>Try again</button></div>
  {:else}
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
