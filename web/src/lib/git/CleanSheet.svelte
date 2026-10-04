<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError, api } from '../api';
  import type { GitActionResult, GitCleanPlan } from '../types';
  import ResultCard from './ResultCard.svelte';
  import Sheet from './Sheet.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  let { projectId, store, worktreeId, label, head }: { projectId: string; store: GitStore; worktreeId: string; label: string; head: string } = $props();

  let plan = $state<GitCleanPlan | null>(null);
  let error = $state('');
  let result = $state<GitActionResult | null>(null);
  let busy = $state(false);

  $effect(() => {
    untrack(() => {
      api.git.cleanPlan(projectId, worktreeId, head).then(
        (p) => (plan = p),
        (err) => (error = err instanceof ApiError ? err.message : String(err)),
      );
    });
  });

  async function clean() {
    busy = true;
    error = '';
    try {
      result = await api.git.clean(projectId, worktreeId, head);
      store.lastResult = result;
      await store.reloadAll();
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet title="Clean up worktree" onclose={() => sheets.close()}>
  {#if result}
    <ResultCard {result} />
  {:else if plan}
    <p class="g-wrap">
      Remove the directory Dev Board made for <strong>{label}</strong>.
    </p>
    <p class="mono g-small g-wrap">{plan.path}</p>
    <p class="g-small muted">
      The branch is kept, with all its commits. Only a directory with nothing uncommitted in it, that no run is using, is ever removed:
      Dev Board never discards uncommitted work.
    </p>
    {#if plan.missing}<p class="g-notice g-small">The directory is already gone; this only forgets the record of it.</p>{/if}
    {#if plan.blockers.length}
      <div class="g-notice" data-tone="bad" role="alert">
        <strong>It will not be removed</strong>
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
  {:else if !error}
    <p class="muted">Checking the worktree now…</p>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  {#snippet footer()}
    {#if result}
      <button class="btn primary" onclick={() => sheets.close()}>Done</button>
    {:else}
      <button class="btn" onclick={() => sheets.close()}>Cancel</button>
      <button class="btn danger" disabled={!plan?.canClean || busy} onclick={clean}>{busy ? 'Removing…' : 'Remove worktree'}</button>
    {/if}
  {/snippet}
</Sheet>
