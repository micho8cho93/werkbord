<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError, api } from '../api';
  import { shortSha } from '../gitui';
  import type { GitActionResult } from '../types';
  import ResultCard from './ResultCard.svelte';
  import Sheet from '../Sheet.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  let { projectId, store, branchName }: { projectId: string; store: GitStore; branchName: string } = $props();

  const branch = untrack(() => store.overview?.branches.find((b) => b.scope === 'local' && b.name === branchName));
  const target = untrack(() => store.overview?.local.target.name ?? '');
  const repo = untrack(() => store.overview?.remote.github.repo ?? '');

  let title = $state(untrack(() => branch?.devboard.taskTitle || branch?.subject || ''));
  let body = $state('');
  let draft = $state(true);
  let result = $state<GitActionResult | null>(null);
  let busy = $state(false);
  let error = $state('');

  async function create() {
    if (!branch) return;
    busy = true;
    error = '';
    try {
      result = await api.git.createPullRequest(projectId, { branch: branch.name, expectedSha: branch.sha, title, body, draft });
      store.lastResult = result;
      await store.reloadAll();
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet title="Open a pull request" onclose={() => sheets.close()}>
  {#if result}
    <ResultCard {result} />
  {:else if !branch}
    <p class="error">This branch is no longer in the list. Refresh and try again.</p>
  {:else}
    <p class="g-wrap">
      <strong>{branch.name}</strong> into <strong>{target}</strong>{#if repo} on GitHub ({repo}){/if}, using your own GitHub CLI.
    </p>
    <label class="field">
      <span class="g-small muted">Title</span>
      <input class="input" bind:value={title} maxlength="256" />
    </label>
    <label class="field">
      <span class="g-small muted">Description</span>
      <textarea class="input" rows="4" bind:value={body}></textarea>
    </label>
    <label class="check"><input type="checkbox" bind:checked={draft} /> Open as a draft</label>
    <p class="g-small muted">
      The branch must already be on the remote at <span class="mono">{shortSha(branch.sha)}</span>: this never pushes for you. The pull request
      is read back from GitHub before it is reported as opened.
    </p>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  {#snippet footer()}
    {#if result}
      <button class="btn primary" onclick={() => sheets.close()}>Done</button>
    {:else}
      <button class="btn" onclick={() => sheets.close()}>Cancel</button>
      <button class="btn primary" disabled={!branch || busy || !title.trim()} onclick={create}>{busy ? 'Opening…' : 'Open pull request'}</button>
    {/if}
  {/snippet}
</Sheet>

<style>
  .field {
    display: grid;
    gap: 4px;
  }

  .check {
    display: flex;
    gap: 8px;
    align-items: center;
    min-height: 36px;
  }
</style>
