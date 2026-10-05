<script lang="ts">
  import { ApiError, api } from '../api';
  import { timeAgo } from '../format';
  import { changesHref, commitsHref, fileHref, overviewHref } from '../gitroute';
  import {
    branchActions,
    branchChips,
    dirtyTotal,
    mergeableLabel,
    checksLabel,
    originLabel,
    phaseLabel,
    prStateLabel,
    relationLabel,
    reviewLabel,
    shortSha,
    splitPath,
    statusGlyph,
    statusWord,
    upstreamLabel,
    openPRsByBranch,
    safeURL,
    type BranchAction,
  } from '../gitui';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { BranchScope, GitComparison, GitDiffFile, Project } from '../types';
  import Chips from './Chips.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  // One branch, and the way down from it: its state and actions, the commits only it has,
  // then its changed files, each of which opens that file's diff. Files come a page at a
  // time, so a branch that changes thousands of them is as light as one that changes two.

  let { project, store, scope, name }: { project: Project; store: GitStore; scope: BranchScope; name: string } = $props();

  const o = $derived(store.overview);
  const branch = $derived(o?.branches.find((b) => b.scope === scope && b.name === name));
  const gh = $derived(store.github);
  const pr = $derived(branch ? openPRsByBranch(gh?.pullRequests ?? []).get(branch.name) : undefined);
  const ctx = $derived({ github: !!o?.remote.github.detected && (gh?.available ?? false), hasOpenPR: !!pr });
  const actions = $derived(branch ? branchActions(branch, ctx) : []);

  let cmp = $state<GitComparison | null>(null);
  let files = $state<GitDiffFile[]>([]);
  let loading = $state(false);
  let error = $state('');
  let showMissing = $state(false);

  const PAGE = 50;
  let req = 0;

  async function load() {
    const id = ++req;
    loading = true;
    error = '';
    try {
      const c = await api.git.compare(project.id, scope, name, 0, PAGE);
      if (id !== req) return;
      cmp = c;
      files = c.files;
    } catch (err) {
      if (id === req) error = err instanceof ApiError ? err.message : String(err);
    } finally {
      if (id === req) loading = false;
    }
  }

  async function moreFiles() {
    if (!cmp || loading) return;
    loading = true;
    try {
      const c = await api.git.compare(project.id, scope, name, files.length, PAGE);
      // The branch moved while reading: the first page is read again rather than mixing two states.
      if (c.branchSha !== cmp.branchSha) {
        await load();
        return;
      }
      files = [...files, ...c.files];
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  // Read again whenever the branch is at another commit, or the target is. (Compared as values: a
  // refetch of the overview that changes nothing about this branch does not read the comparison again.)
  const tip = $derived(branch?.sha);
  const targetTip = $derived(o?.local.target.sha);
  $effect(() => {
    void [tip, targetTip, scope, name];
    void load();
  });

  function act(a: BranchAction) {
    if (!branch) return;
    switch (a.id) {
      case 'merge':
      case 'push':
      case 'pr':
      case 'delete':
        sheets.open({ kind: a.id, branch: branch.name });
        break;
      case 'clean':
        if (branch.worktree?.worktreeId) {
          sheets.open({ kind: 'clean', worktreeId: branch.worktree.worktreeId, label: branch.devboard.taskTitle || branch.name, branch: branch.name, head: branch.sha });
        }
        break;
      case 'review':
        document.getElementById('files')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
        break;
    }
  }

  const changing = $derived(actions.filter((a) => a.id !== 'review' && a.id !== 'task' && a.id !== 'run'));
  const hasTask = $derived(!!branch?.devboard.taskId);
  const prState = $derived(pr ? prStateLabel(pr) : null);
</script>

<div class="g-page">
  <a class="g-back" href={overviewHref(project.id)}>← Git</a>

  {#if !branch}
    <p class="card empty">{o ? 'This branch is not in the list any more.' : 'Loading…'}</p>
  {:else}
    <header class="head">
      <h2 class="name g-wrap">{branch.name}</h2>
      <div class="g-row"><Chips chips={branchChips(branch, o?.local.target.name ?? '')} /></div>
    </header>

    <section class="card g-card" aria-label="State">
      <dl class="g-kv">
        <dt>Against</dt>
        <dd>{relationLabel(branch, o?.local.target.name ?? '')}</dd>
        {#if branch.scope === 'local'}
          <dt>Remote</dt>
          <dd>{upstreamLabel(branch.upstream)} <span class="muted g-small">(as of the last fetch)</span></dd>
        {/if}
        <dt>Latest</dt>
        <dd class="g-wrap"><span class="mono">{shortSha(branch.sha)}</span> {branch.subject} <span class="muted g-small">· {timeAgo(branch.commitDate, app.now)}</span></dd>
        <dt>Made by</dt>
        <dd>
          {originLabel(branch)}
          {#if phaseLabel(branch.devboard.phase)}<span class="muted g-small"> · {phaseLabel(branch.devboard.phase)}</span>{/if}
        </dd>
        {#if branch.worktree}
          <dt>Checked out</dt>
          <dd class="g-wrap">
            <span class="mono g-small">{branch.worktree.path}</span>
            {#if branch.worktree.primary}<span class="muted g-small"> · your checkout</span>{/if}
            {#if branch.worktree.missing}<span class="g-chip" data-tone="bad">directory gone</span>{/if}
            {#if branch.worktree.dirty && dirtyTotal(branch.worktree.dirty) > 0}
              <br /><a href={changesHref(project.id, branch.worktree.primary ? '' : (branch.worktree.worktreeId ?? ''))}>{dirtyTotal(branch.worktree.dirty)} uncommitted changes</a>
            {/if}
          </dd>
        {/if}
        {#if pr && prState}
          <dt>Pull request</dt>
          <dd class="g-wrap">
            <a href={safeURL(pr.url)} target="_blank" rel="noopener noreferrer">#{pr.number} {pr.title}</a>
            <span class="g-chip" data-tone={prState.tone}>{prState.text}</span>
            {#if reviewLabel(pr.review)}<span class="g-chip" data-tone={reviewLabel(pr.review)?.tone}>{reviewLabel(pr.review)?.text}</span>{/if}
            {#if checksLabel(pr.checks)}<span class="g-chip" data-tone={checksLabel(pr.checks)?.tone}>{checksLabel(pr.checks)?.text}</span>{/if}
            {#if mergeableLabel(pr.mergeable)}<span class="g-chip" data-tone={mergeableLabel(pr.mergeable)?.tone}>{mergeableLabel(pr.mergeable)?.text}</span>{/if}
          </dd>
        {/if}
      </dl>
      {#if branch.attention.length}
        <ul class="g-bullets">
          {#each branch.attention as a (a.kind)}<li class="g-small g-wrap">{a.message}</li>{/each}
        </ul>
      {/if}
      {#if branch.unusual}<p class="g-notice g-small" data-tone="bad">Werkbord will not act on this branch: {branch.unusual}.</p>{/if}
      {#if branch.protected && branch.scope === 'local'}<p class="g-small muted">Protected: Werkbord never deletes this branch.</p>{/if}
    </section>

    {#if changing.length || hasTask || branch.devboard.runId}
      <section class="g-actions" aria-label="Actions">
        {#each changing as a (a.id)}
          <button class="btn" class:primary={a.primary && !a.danger} class:danger={a.danger} onclick={() => act(a)}>{a.label}</button>
        {/each}
        {#if branch.devboard.taskId}<a class="btn" href={taskHref(project.id, branch.devboard.taskId)}>Open task</a>{/if}
        {#if branch.devboard.runId && branch.devboard.taskId}<a class="btn" href={taskHref(project.id, branch.devboard.taskId)}>Open run</a>{/if}
        {#if branch.scope === 'local'}<a class="btn" href={commitsHref(project.id, 'local', branch.name)}>History</a>{/if}
      </section>
    {/if}

    {#if error}<p class="error" role="alert">{error}</p>{/if}

    {#if cmp}
      <section class="g-section" aria-label="Commits only on this branch">
        <header>
          <h2>Commits on this branch</h2>
          <span class="count">{cmp.unique.total} not in {cmp.target}</span>
        </header>
        {#if cmp.unique.items.length}
          <div class="card commits">
            {#each cmp.unique.items as c (c.sha)}
              <div class="g-commit">
                <span class="subject">{c.subject}</span>
                <span class="meta"><code>{shortSha(c.sha)}</code> · {c.author} · {timeAgo(c.date, app.now)}</span>
              </div>
            {/each}
          </div>
          {#if cmp.unique.truncated}<p class="g-small muted">Showing the newest {cmp.unique.items.length} of {cmp.unique.total}.</p>{/if}
        {:else}
          <p class="card empty">No commits beyond {cmp.target}.</p>
        {/if}

        {#if cmp.missing.total > 0}
          <button class="btn quiet small" onclick={() => (showMissing = !showMissing)}>
            {showMissing ? 'Hide' : 'Show'} {cmp.missing.total} commit{cmp.missing.total === 1 ? '' : 's'} on {cmp.target} that this branch lacks
          </button>
          {#if showMissing}
            <div class="card commits">
              {#each cmp.missing.items as c (c.sha)}
                <div class="g-commit">
                  <span class="subject">{c.subject}</span>
                  <span class="meta"><code>{shortSha(c.sha)}</code> · {c.author} · {timeAgo(c.date, app.now)}</span>
                </div>
              {/each}
            </div>
          {/if}
        {/if}
      </section>

      <section class="g-section" id="files" aria-label="Changed files">
        <header>
          <h2>Changed files</h2>
          <span class="count">
            {cmp.filesTotal}{cmp.truncated ? '+' : ''} · <span class="add">+{cmp.additions}</span> <span class="del">−{cmp.deletions}</span>{#if cmp.binaryFiles} · {cmp.binaryFiles} binary{/if}
          </span>
        </header>
        <p class="g-small muted">{cmp.basis}. Tap a file to see its diff.</p>
        {#if files.length}
          <div class="card list">
            {#each files as f (f.path)}
              {@const p = splitPath(f.path)}
              <a class="g-file" href={fileHref(project.id, scope, name, f.path, f.oldPath ?? '')}>
                <span class="g-glyph" data-status={f.status} title={statusWord(f.status)}>{statusGlyph(f.status)}</span>
                <span class="g-path"><span class="dir">{p.dir}</span>{p.name}{#if f.oldPath}<span class="muted g-small"> ← {f.oldPath}</span>{/if}</span>
                <span class="g-nums">{#if f.binary}<span class="muted">binary</span>{:else}<span class="add">+{f.additions}</span> <span class="del">−{f.deletions}</span>{/if}</span>
              </a>
            {/each}
          </div>
          {#if files.length < cmp.filesTotal}
            <button class="btn" onclick={moreFiles} disabled={loading}>{loading ? 'Loading…' : `Show more files (${cmp.filesTotal - files.length} left)`}</button>
          {/if}
          {#if cmp.truncated}<p class="g-notice g-small">This branch changes more files than can be listed here ({cmp.filesTotal}+).</p>{/if}
        {:else}
          <p class="card empty">No files differ from {cmp.target}.</p>
        {/if}
      </section>
    {:else if loading}
      <p class="card empty">Comparing with the target…</p>
    {/if}
  {/if}
</div>

<style>
  .head {
    display: grid;
    gap: 6px;
  }

  .name {
    font-size: 1.1rem;
    text-transform: none;
    letter-spacing: 0;
    color: var(--text);
    font-weight: 650;
  }

  .commits,
  .list {
    overflow: hidden;
  }

  .add {
    color: var(--ok);
    font-family: var(--mono);
  }

  .del {
    color: var(--danger);
    font-family: var(--mono);
  }
</style>
