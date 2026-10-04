<script lang="ts">
  import { api } from '../lib/api';
  import { app } from '../lib/state.svelte';
  import type { Project } from '../lib/types';

  let { project }: { project: Project } = $props();

  let refreshing = $state(false);
  let error = $state('');

  async function refresh() {
    refreshing = true;
    error = '';
    try {
      app.upsertProject(await api.refreshProject(project.id));
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      refreshing = false;
    }
  }

  const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });
  const repo = $derived(project.repository);
</script>

<div class="layout">
  <article class="card repo">
    <header>
      <h2>Repository</h2>
      <button class="btn" onclick={refresh} disabled={refreshing}>{refreshing ? 'Refreshing…' : 'Refresh'}</button>
    </header>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <dl>
      <dt>Path</dt>
      <dd class="mono">{project.repoPath}</dd>
      {#if repo}
        <dt>Branch</dt>
        <dd>{repo.currentBranch || 'detached HEAD'}</dd>
        <dt>HEAD</dt>
        <dd class="mono">{repo.headCommit ? repo.headCommit.slice(0, 12) : 'no commits'}</dd>
        {#if repo.defaultBranch}
          <dt>Default</dt>
          <dd>{repo.defaultBranch}</dd>
        {/if}
        <dt>Remotes</dt>
        <dd>
          {#each repo.remotes as r (r.name)}
            <div class="mono">{r.name} · {r.url}</div>
          {:else}
            <span class="muted">none</span>
          {/each}
        </dd>
        <dt>Inspected</dt>
        <dd>{fmt.format(new Date(repo.inspectedAt))}</dd>
      {:else}
        <dt>State</dt>
        <dd class="muted">Not inspected yet.</dd>
      {/if}
    </dl>
  </article>

  <p class="muted note">
    Each run works in its own worktree on a branch named <code>devboard/…</code>, so this checkout is never touched. Diffs, commits and
    pushing are not built yet.
  </p>
</div>

<style>
  .layout {
    display: grid;
    gap: 14px;
    max-width: 44rem;
  }

  .repo {
    padding: 14px 16px;
    min-width: 0;
  }

  .repo header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 10px;
  }

  dl {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 6px 14px;
    margin: 0;
    font-size: 0.88rem;
  }

  dt {
    color: var(--text-2);
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .note {
    font-size: 0.85rem;
  }
</style>
