<script lang="ts">
  import { ApiError, api } from '../api';
  import { timeAgo } from '../format';
  import { branchHref, overviewHref } from '../gitroute';
  import { shortSha } from '../gitui';
  import { app } from '../state.svelte';
  import type { BranchScope, GitCommit, Project } from '../types';

  let { project, scope, name }: { project: Project; scope: BranchScope; name: string } = $props();

  let commits = $state<GitCommit[]>([]);
  let more = $state(false);
  let loading = $state(false);
  let error = $state('');
  const PAGE = 30;
  let req = 0;

  async function load(reset: boolean) {
    const id = reset ? ++req : req;
    loading = true;
    error = '';
    try {
      const page = await api.git.commits(project.id, scope, name, reset ? 0 : commits.length, PAGE);
      if (id !== req) return;
      commits = reset ? page.items : [...commits, ...page.items];
      more = page.truncated;
    } catch (err) {
      if (id === req) error = err instanceof ApiError ? err.message : String(err);
    } finally {
      if (id === req) loading = false;
    }
  }

  $effect(() => {
    void [scope, name];
    commits = [];
    void load(true);
  });
</script>

<div class="g-page">
  <a class="g-back" href={overviewHref(project.id)}>← Git</a>
  <header>
    <h2>History</h2>
    <p class="g-wrap"><a href={branchHref(project.id, scope, name)}>{name}</a></p>
  </header>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if commits.length}
    <div class="card commits">
      {#each commits as c (c.sha)}
        <div class="g-commit">
          <span class="subject">{c.subject}</span>
          <span class="meta"><code>{shortSha(c.sha)}</code> · {c.author} · {timeAgo(c.date, app.now)}{#if c.merge} · merge{/if}</span>
        </div>
      {/each}
    </div>
    {#if more}<button class="btn" onclick={() => load(false)} disabled={loading}>{loading ? 'Loading…' : 'Older commits'}</button>{/if}
  {:else if !loading && !error}
    <p class="card empty">No commits.</p>
  {:else if loading}
    <p class="card empty">Loading…</p>
  {/if}
</div>

<style>
  .commits {
    overflow: hidden;
  }
</style>
