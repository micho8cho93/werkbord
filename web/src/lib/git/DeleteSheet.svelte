<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError, api } from '../api';
  import { shortSha } from '../gitui';
  import type { GitActionResult, GitDeletePlan } from '../types';
  import ResultCard from './ResultCard.svelte';
  import Sheet from '../Sheet.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  let { projectId, store, branchName }: { projectId: string; store: GitStore; branchName: string } = $props();

  const reviewedSha = untrack(() => store.overview?.branches.find((b) => b.scope === 'local' && b.name === branchName)?.sha ?? '');

  let alsoRemote = $state(false);
  let plan = $state<GitDeletePlan | null>(null);
  let planning = $state(false);
  let result = $state<GitActionResult | null>(null);
  let busy = $state(false);
  let error = $state('');

  const request = $derived({ branch: branchName, branchSha: reviewedSha, deleteRemote: alsoRemote });

  async function check() {
    planning = true;
    error = '';
    try {
      plan = await api.git.deletePlan(projectId, request);
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      planning = false;
    }
  }

  $effect(() => {
    void alsoRemote;
    untrack(() => void check());
  });

  async function remove() {
    busy = true;
    error = '';
    try {
      result = await api.git.deleteBranch(projectId, request);
      store.lastResult = result;
      await store.reloadAll();
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet title="Delete {branchName}" onclose={() => sheets.close()}>
  {#if result}
    <ResultCard {result} />
  {:else if planning && !plan}
    <p class="muted">Checking the repository now…</p>
  {:else if plan}
    <p class="g-wrap">
      Delete the local branch <strong>{plan.branch}</strong> (at <span class="mono">{shortSha(plan.branchSha)}</span>).
    </p>
    {#if plan.merged}
      <p class="g-notice g-small" data-tone="ok">
        {#if plan.mergedVia === 'pull_request'}
          Its tip was merged by a pull request on GitHub, so its work is in {plan.target}.
        {:else}
          Every commit on it is already in {plan.target}.
        {/if}
        The commits stay recoverable by their ID.
      </p>
    {/if}
    {#if plan.remoteExists}
      <label class="check">
        <input type="checkbox" bind:checked={alsoRemote} disabled={!plan.canDeleteRemote && !alsoRemote} />
        <span>Also delete <span class="mono">{plan.remoteRef}</span> on the remote</span>
      </label>
      {#if !plan.canDeleteRemote}
        <p class="g-small muted">The remote copy cannot be deleted from here: it is not clearly merged, or the local branch cannot be deleted.</p>
      {/if}
    {/if}
    {#if plan.blockers.length}
      <div class="g-notice" data-tone="bad" role="alert">
        <strong>It will not be deleted</strong>
        <ul class="g-bullets">
          {#each plan.blockers as b (b.code + b.message)}<li class="g-wrap">{b.message}</li>{/each}
        </ul>
      </div>
    {/if}
    {#if plan.warnings.length}
      <div class="g-notice">
        <ul class="g-bullets">
          {#each plan.warnings as w (w)}<li class="g-wrap">{w}</li>{/each}
        </ul>
      </div>
    {/if}
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  {#snippet footer()}
    {#if result}
      <button class="btn primary" onclick={() => sheets.close()}>Done</button>
    {:else}
      <button class="btn" onclick={() => sheets.close()}>Cancel</button>
      <button class="btn danger" disabled={!plan?.canDelete || busy || planning} onclick={remove}>{busy ? 'Deleting…' : 'Delete branch'}</button>
    {/if}
  {/snippet}
</Sheet>

<style>
  .check {
    display: flex;
    gap: 8px;
    align-items: baseline;
    min-height: 36px;
  }
</style>
