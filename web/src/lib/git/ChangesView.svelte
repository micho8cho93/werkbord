<script lang="ts">
  import { ApiError, api } from '../api';
  import { changeHref, overviewHref } from '../gitroute';
  import { splitPath, statusGlyph, statusWord } from '../gitui';
  import { taskHref } from '../router.svelte';
  import type { GitFileChange, Project, WorkingChanges } from '../types';
  import type { ChangeKind } from '../gitroute';

  // What is uncommitted in one checkout: the project's own, or a worktree Werkbord made.
  // An agent's unfinished work lives here, not on its branch, until it is committed.

  let { project, worktree }: { project: Project; worktree: string } = $props();

  let data = $state<WorkingChanges | null>(null);
  let error = $state('');
  let loading = $state(false);

  async function load() {
    loading = true;
    error = '';
    try {
      data = await api.git.changes(project.id, worktree);
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void worktree;
    data = null;
    void load();
  });

  const groups = $derived(
    data
      ? ([
          { kind: 'staged', title: 'Staged', note: 'Will be in the next commit', files: data.tree.staged },
          { kind: 'unstaged', title: 'Modified, not staged', note: 'Changed but not added', files: data.tree.unstaged },
          { kind: 'untracked', title: 'Untracked', note: 'New files Git is not tracking', files: data.tree.untracked },
        ] as { kind: ChangeKind; title: string; note: string; files: GitFileChange[] }[])
      : [],
  );
</script>

<div class="g-page">
  <a class="g-back" href={overviewHref(project.id)}>← Git</a>
  {#if error}
    <p class="error" role="alert">{error}</p>
  {:else if !data}
    <p class="card empty">Loading…</p>
  {:else}
    <header>
      <h2>Working changes</h2>
      <p class="g-wrap">
        {#if data.primary}Your checkout{:else}Worktree of <a href={data.taskId ? taskHref(project.id, data.taskId) : '#'}>{data.taskTitle || 'a task'}</a>{/if}
        · <strong>{data.tree.detached ? 'detached HEAD' : data.tree.branch}</strong>
      </p>
      <p class="mono g-small muted g-wrap">{data.path}</p>
      <div class="g-row">
        <button class="btn small" onclick={load} disabled={loading}>{loading ? 'Refreshing…' : 'Refresh'}</button>
      </div>
    </header>

    {#if data.tree.operation}<p class="g-notice" data-tone="bad">A {data.tree.operation} is unfinished here. Werkbord will not merge into, or remove, a checkout in this state.</p>{/if}

    {#if data.tree.conflicted.length}
      <section class="g-section">
        <header><h2>In conflict</h2><span class="count">{data.tree.counts.conflicted}</span></header>
        <div class="card list">
          {#each data.tree.conflicted as f (f.path)}
            <div class="g-file static">
              <span class="g-glyph" data-status="conflicted">!</span>
              <span class="g-path">{f.path}</span>
              <span></span>
            </div>
          {/each}
        </div>
      </section>
    {/if}

    {#each groups as g (g.kind)}
      {#if g.files.length}
        <section class="g-section">
          <header><h2>{g.title}</h2><span class="count">{data.tree.counts[g.kind]}</span></header>
          <p class="g-small muted">{g.note}. Tap a file for its diff.</p>
          <div class="card list">
            {#each g.files as f (f.path + f.status)}
              {@const p = splitPath(f.path)}
              <a class="g-file" href={changeHref(project.id, data.worktreeId ?? '', g.kind, f.path, f.oldPath ?? '')}>
                <span class="g-glyph" data-status={f.status} title={statusWord(f.status)}>{statusGlyph(f.status)}</span>
                <span class="g-path"><span class="dir">{p.dir}</span>{p.name}{#if f.oldPath}<span class="muted g-small"> ← {f.oldPath}</span>{/if}</span>
                <span class="muted g-small">{statusWord(f.status)}</span>
              </a>
            {/each}
          </div>
          {#if data.tree.counts[g.kind] > g.files.length}
            <p class="g-small muted">Showing {g.files.length} of {data.tree.counts[g.kind]}.</p>
          {/if}
        </section>
      {/if}
    {/each}

    {#if data.tree.clean}
      <p class="card empty">Nothing is uncommitted here.</p>
    {/if}
  {/if}
</div>

<style>
  .list {
    overflow: hidden;
  }

  .static {
    cursor: default;
  }
</style>
