<script lang="ts">
  import { api } from '../api';
  import { branchHref } from '../gitroute';
  import { splitPath } from '../gitui';
  import type { BranchScope, GitComparison, Project } from '../types';
  import FileDiffView from './FileDiffView.svelte';

  let { project, scope, name, path, oldPath }: { project: Project; scope: BranchScope; name: string; path: string; oldPath: string } = $props();

  // The diff is pinned to the two commits the comparison reports, so the lines paged in
  // are all of one diff even if the branch moves while they are being read.
  let base = $state<GitComparison | null>(null);
  let error = $state('');

  $effect(() => {
    base = null;
    error = '';
    api.git.compare(project.id, scope, name, 0, 1).then(
      (c) => (base = c),
      (err) => (error = err instanceof Error ? err.message : String(err)),
    );
  });

  const p = $derived(splitPath(path));
</script>

<div class="g-page">
  <a class="g-back" href={branchHref(project.id, scope, name)}>← {name}</a>
  <header>
    <h2 class="path g-wrap"><span class="dir">{p.dir}</span>{p.name}</h2>
    {#if oldPath}<p class="g-small muted g-wrap">renamed from {oldPath}</p>{/if}
  </header>
  {#if error}
    <p class="error" role="alert">{error}</p>
  {:else if base}
    {@const b = base}
    <FileDiffView
      label={`${b.mergeBase ?? b.targetSha}..${b.branchSha}:${path}`}
      load={(offset) => api.git.diff(project.id, b.mergeBase || b.targetSha, b.branchSha, path, oldPath, offset, 400)}
    />
  {:else}
    <p class="card empty">Loading…</p>
  {/if}
</div>

<style>
  .path {
    font-size: 0.95rem;
    text-transform: none;
    letter-spacing: 0;
    color: var(--text);
    font-family: var(--mono);
    font-weight: 600;
  }

  .dir {
    color: var(--text-2);
    font-weight: 500;
  }
</style>
