<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError, api } from '../api';
  import { shortSha, upstreamLabel } from '../gitui';
  import type { GitActionResult } from '../types';
  import ResultCard from './ResultCard.svelte';
  import Sheet from './Sheet.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  let { projectId, store, branchName }: { projectId: string; store: GitStore; branchName: string } = $props();

  // Pinned to the commit on screen: if the branch has moved by the time this is confirmed, it is refused.
  const branch = untrack(() => store.overview?.branches.find((b) => b.scope === 'local' && b.name === branchName));
  const remote = untrack(() => store.overview?.remote.remotes.find((r) => r.name === 'origin')?.name ?? store.overview?.remote.remotes[0]?.name ?? 'the remote');

  let result = $state<GitActionResult | null>(null);
  let busy = $state(false);
  let error = $state('');

  async function push() {
    if (!branch) return;
    busy = true;
    error = '';
    try {
      result = await api.git.push(projectId, branch.name, branch.sha);
      store.lastResult = result;
      await store.reloadAll();
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet title="Push {branchName}" onclose={() => sheets.close()}>
  {#if result}
    <ResultCard {result} />
  {:else if !branch}
    <p class="error">This branch is no longer in the list. Refresh and try again.</p>
  {:else}
    <p class="g-wrap">Push <strong>{branch.name}</strong> to <strong>{remote}</strong> at <span class="mono">{shortSha(branch.sha)}</span>.</p>
    <dl class="g-kv">
      <dt>Now</dt>
      <dd>{upstreamLabel(branch.upstream)} <span class="muted g-small">(as of the last fetch)</span></dd>
      {#if branch.notPushed}
        <dt>To push</dt>
        <dd>{branch.notPushed} commit{branch.notPushed === 1 ? '' : 's'} that exist on no remote</dd>
      {/if}
    </dl>
    <p class="g-small muted">
      A plain push: never forced. If the remote has commits this branch lacks, it is refused and nothing is overwritten. Afterwards the
      remote itself is asked to confirm the branch is where it should be.
    </p>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  {#snippet footer()}
    {#if result}
      <button class="btn primary" onclick={() => sheets.close()}>Done</button>
    {:else}
      <button class="btn" onclick={() => sheets.close()}>Cancel</button>
      <button class="btn primary" disabled={!branch || busy} onclick={push}>{busy ? 'Pushing…' : `Push to ${remote}`}</button>
    {/if}
  {/snippet}
</Sheet>
