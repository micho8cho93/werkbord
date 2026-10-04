<script lang="ts">
  import { ApiError, api } from '../api';
  import { timeAgo } from '../format';
  import { changesHref, commitsHref } from '../gitroute';
  import {
    BRANCH_FILTERS,
    safeURL,
    checksLabel,
    dirtyTotal,
    fetchedLabel,
    filterBranches,
    githubUnavailable,
    mergeableLabel,
    needsAttention,
    openPRsByBranch,
    prStateLabel,
    reviewLabel,
    shortSha,
    upstreamLabel,
    type BranchFilter,
  } from '../gitui';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { Project } from '../types';
  import BranchCard from './BranchCard.svelte';
  import Chips from './Chips.svelte';
  import ResultCard from './ResultCard.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  let { project, store }: { project: Project; store: GitStore } = $props();

  const o = $derived(store.overview);
  const gh = $derived(store.github);
  const ctx = $derived({
    github: !!o?.remote.github.detected && (gh?.available ?? false),
    hasOpenPR: false,
  });
  const openPRs = $derived(openPRsByBranch(gh?.pullRequests ?? []));
  const ctxFor = (name: string) => ({ ...ctx, hasOpenPR: openPRs.has(name) });

  const attention = $derived((o?.branches ?? []).filter(needsAttention));
  const SHOW_ATTENTION = 6;
  let showAllAttention = $state(false);
  // Dev Board's own branches first, unless there are none, when everything is shown.
  let chosen = $state<BranchFilter | null>(null);
  const filter = $derived<BranchFilter>(chosen ?? ((o?.summary.devboard ?? 0) > 0 ? 'devboard' : 'all'));
  const listed = $derived(filterBranches(o?.branches ?? [], filter));
  const SHOW_BRANCHES = 12;
  let showAllBranches = $state(false);

  let fetching = $state(false);
  let fetchError = $state('');

  async function fetchRemote() {
    fetching = true;
    fetchError = '';
    try {
      store.lastResult = await api.git.fetch(project.id);
      await store.reloadAll();
    } catch (err) {
      fetchError = err instanceof ApiError ? err.message : String(err);
    } finally {
      fetching = false;
    }
  }

  const tree = $derived(o?.local.workingTree);
  const dirty = $derived(tree ? dirtyTotal(tree.counts) : 0);
  const sync = $derived(o?.remote.sync);
  const openPRList = $derived((gh?.pullRequests ?? []).filter((p) => p.state === 'open'));
  const recentPRs = $derived((gh?.pullRequests ?? []).filter((p) => p.state !== 'open').slice(0, 5));
</script>

{#if store.error && !o}
  <div class="card g-card">
    <p class="error" role="alert">{store.error}</p>
    <button class="btn" onclick={() => store.load()}>Try again</button>
  </div>
{:else if !o}
  <p class="card empty">Loading the repository…</p>
{:else}
  <div class="g-page">
    {#if store.lastResult}
      <div class="result">
        <ResultCard result={store.lastResult} />
        <button class="btn small quiet" onclick={() => (store.lastResult = null)}>Dismiss</button>
      </div>
    {/if}

    <!-- 1. Repository summary: this computer, and the remote, apart. -->
    <section class="card g-card summary" aria-label="Repository">
      <header class="head">
        <div class="g-wrap">
          <h2>Repository</h2>
          <p class="title">{o.local.name}</p>
        </div>
        <div class="g-row">
          <button class="btn small" onclick={() => store.reloadAll()} disabled={store.loading} title="Look at the repository again. This does not use the network.">
            {store.loading ? 'Refreshing…' : 'Refresh'}
          </button>
          <button class="btn small" onclick={fetchRemote} disabled={fetching || !o.remote.remotes.length} title="Fetch from the remote to update what is known about it. Changes no branch of yours.">
            {fetching ? 'Fetching…' : 'Fetch'}
          </button>
        </div>
      </header>
      {#if fetchError}<p class="error" role="alert">{fetchError}</p>{/if}

      <div class="scopes">
        <div class="g-scope">
          <h3>On this computer</h3>
          <dl class="g-kv">
            <dt>HEAD</dt>
            <dd>
              {#if o.local.head.unborn}<span class="muted">no commits yet</span>
              {:else if o.local.head.detached}<strong>detached</strong> at <span class="mono">{shortSha(o.local.head.commit)}</span>
              {:else}<strong>{o.local.head.branch}</strong> <span class="mono muted">{shortSha(o.local.head.commit)}</span>{/if}
              {#if o.local.head.subject}<span class="muted g-small"> · {o.local.head.subject}</span>{/if}
            </dd>
            <dt>Target</dt>
            <dd>
              {#if o.local.target.name}
                <strong>{o.local.target.name}</strong>
                <span class="muted g-small">· from {o.local.target.source}</span>
                {#if !o.local.target.localExists}<span class="g-chip" data-tone="ask">remote only</span>{/if}
                {#if o.local.target.localExists && o.local.target.upstream.state !== 'none'}
                  <span class="muted g-small"> · {upstreamLabel(o.local.target.upstream)}</span>
                {/if}
              {:else}<span class="muted">none found</span>{/if}
            </dd>
            <dt>Working tree</dt>
            <dd>
              {#if tree?.operation}<span class="g-chip" data-tone="ask">{tree.operation} in progress</span>{/if}
              {#if dirty === 0}
                <span class="ok">clean</span>
              {:else}
                <a href={changesHref(project.id)}>
                  {tree?.counts.staged} staged · {tree?.counts.unstaged} modified · {tree?.counts.untracked} untracked{#if tree?.counts.conflicted} · {tree?.counts.conflicted} in conflict{/if}
                </a>
              {/if}
            </dd>
          </dl>
        </div>

        <div class="g-scope">
          <h3>On the remote <span class="when">{fetchedLabel(o.remote.lastFetchedAt, app.now)}</span></h3>
          <dl class="g-kv">
            <dt>Remotes</dt>
            <dd>
              {#each o.remote.remotes as r (r.name)}<div class="mono g-small">{r.name} · {r.url}</div>{:else}<span class="muted">none configured</span>{/each}
            </dd>
            {#if sync}
              <dt>This branch</dt>
              <dd>
                {upstreamLabel(sync.upstream)}
                <span class="muted g-small">· as of the last fetch</span>
              </dd>
            {/if}
            {#if o.remote.github.detected}
              <dt>GitHub</dt>
              <dd class="g-wrap">{o.remote.github.repo}</dd>
            {/if}
          </dl>
        </div>
      </div>

      {#if sync && (sync.notPushed.total > 0 || sync.notPulled.total > 0)}
        <div class="sync">
          {#if sync.notPushed.total > 0}
            <details>
              <summary>{sync.notPushed.total} commit{sync.notPushed.total === 1 ? '' : 's'} not pushed</summary>
              {#each sync.notPushed.items as c (c.sha)}<div class="g-small"><span class="mono">{shortSha(c.sha)}</span> {c.subject}</div>{/each}
            </details>
          {/if}
          {#if sync.notPulled.total > 0}
            <details>
              <summary>{sync.notPulled.total} commit{sync.notPulled.total === 1 ? '' : 's'} on the remote not pulled</summary>
              {#each sync.notPulled.items as c (c.sha)}<div class="g-small"><span class="mono">{shortSha(c.sha)}</span> {c.subject}</div>{/each}
              <p class="g-small muted">Dev Board does not pull. Update your checkout from your terminal.</p>
            </details>
          {/if}
        </div>
      {/if}

      <div class="counts g-row" aria-label="Summary">
        <Chips chips={[
          { text: `${o.summary.devboard} Dev Board branch${o.summary.devboard === 1 ? '' : 'es'}`, tone: 'work' },
          ...(o.summary.mergeable ? [{ text: `${o.summary.mergeable} ready to review`, tone: 'ask' as const }] : []),
          ...(o.summary.cleanup ? [{ text: `${o.summary.cleanup} to clean up`, tone: 'ok' as const }] : []),
          ...(o.summary.unpushed ? [{ text: `${o.summary.unpushed} with unpushed work`, tone: 'ask' as const }] : []),
          ...(o.summary.dirtyTrees ? [{ text: `${o.summary.dirtyTrees} worktree${o.summary.dirtyTrees === 1 ? '' : 's'} with uncommitted work`, tone: 'ask' as const }] : []),
        ]} />
      </div>

      {#if o.notes?.length}
        <ul class="g-bullets notes">
          {#each o.notes as n (n)}<li class="g-small muted g-wrap">{n}</li>{/each}
        </ul>
      {/if}
    </section>

    <!-- 2. Branches needing attention: Dev Board's own first. -->
    <section class="g-section" aria-label="Branches needing attention">
      <header>
        <h2>Needs you</h2>
        <span class="count">{attention.length}</span>
      </header>
      {#if attention.length}
        <ul class="g-list">
          {#each showAllAttention ? attention : attention.slice(0, SHOW_ATTENTION) as b (b.ref)}
            <BranchCard projectId={project.id} branch={b} targetName={o.local.target.name} ctx={ctxFor(b.name)} emphasis={b.devboard.created} />
          {/each}
        </ul>
        {#if attention.length > SHOW_ATTENTION && !showAllAttention}
          <button class="btn quiet" onclick={() => (showAllAttention = true)}>Show all {attention.length}</button>
        {/if}
      {:else}
        <p class="card empty">Nothing needs you. No branch is waiting for a decision.</p>
      {/if}
    </section>

    <!-- 3. Pull requests, from GitHub, asked for separately. -->
    <section class="g-section" aria-label="Pull requests">
      <header>
        <h2>Pull requests</h2>
        {#if o.remote.github.detected || gh}
          <button class="btn small quiet" onclick={() => store.loadGitHub()} disabled={store.githubLoading}>
            {store.githubLoading ? 'Asking GitHub…' : gh ? 'Refresh' : 'Load from GitHub'}
          </button>
        {/if}
      </header>
      {#if !gh}
        <p class="card empty muted">
          {#if o.remote.github.detected}
            Pull requests come from GitHub, through your own GitHub CLI. {store.githubLoading ? 'Asking GitHub…' : 'Tap Load.'}
          {:else if o.remote.remotes.length === 0}
            This repository has no remote, so there are no pull requests.
          {:else}
            The remote does not look like a GitHub repository.
          {/if}
        </p>
      {:else if !gh.available}
        <p class="card g-card muted g-small">{githubUnavailable(gh)}</p>
      {:else if gh.pullRequests.length === 0}
        <p class="card empty">No pull requests.</p>
      {:else}
        <ul class="g-list">
          {#each [...openPRList, ...recentPRs] as pr (pr.number)}
            {@const st = prStateLabel(pr)}
            {@const checks = checksLabel(pr.checks)}
            {@const review = reviewLabel(pr.review)}
            {@const mergeable = mergeableLabel(pr.mergeable)}
            <li class="card g-card pr">
              <div class="g-row">
                <span class="g-chip" data-tone={st.tone}>{st.text}</span>
                <a class="prtitle g-wrap" href={safeURL(pr.url)} target="_blank" rel="noopener noreferrer">#{pr.number} {pr.title}</a>
              </div>
              <p class="g-small muted g-wrap">
                {pr.headBranch}{pr.crossRepo ? ' (fork)' : ''} → {pr.baseBranch}
                {#if pr.updatedAt}· {timeAgo(pr.updatedAt, app.now)}{/if}
              </p>
              <div class="g-row">
                {#if review}<span class="g-chip" data-tone={review.tone}>{review.text}</span>{/if}
                {#if checks}<span class="g-chip" data-tone={checks.tone}>{checks.text}</span>{/if}
                {#if mergeable}<span class="g-chip" data-tone={mergeable.tone}>{mergeable.text}</span>{/if}
              </div>
              {#if pr.taskId}
                <p class="g-small"><a href={taskHref(project.id, pr.taskId)}>{pr.taskTitle || 'Open task'}</a></p>
              {/if}
            </li>
          {/each}
        </ul>
        <p class="g-small muted">From GitHub {timeAgo(gh.fetchedAt, app.now)}. A merge on GitHub shows here, not from what happened on this computer.</p>
      {/if}
    </section>

    <!-- 4. Recent commits. -->
    <section class="g-section" aria-label="Recent commits">
      <header>
        <h2>Recent commits</h2>
        {#if o.local.head.branch}<a class="g-small" href={commitsHref(project.id, 'local', o.local.head.branch)}>All history</a>{/if}
      </header>
      {#if o.local.recentCommits.length}
        <div class="card commits">
          {#each o.local.recentCommits as c (c.sha)}
            <div class="g-commit">
              <span class="subject">{c.subject}</span>
              <span class="meta"><code>{shortSha(c.sha)}</code> · {c.author} · {timeAgo(c.date, app.now)}{#if c.merge} · merge{/if}</span>
            </div>
          {/each}
        </div>
      {:else}
        <p class="card empty">No commits yet.</p>
      {/if}
    </section>

    <!-- 5. Working changes. -->
    <section class="g-section" aria-label="Working changes">
      <header><h2>Working changes</h2></header>
      <a class="card g-card changes" href={changesHref(project.id)}>
        {#if dirty === 0}
          <span>Nothing uncommitted in your checkout.</span>
        {:else}
          <span><strong>{dirty}</strong> uncommitted: {tree?.counts.staged} staged, {tree?.counts.unstaged} modified, {tree?.counts.untracked} untracked</span>
          <span class="muted g-small">Review →</span>
        {/if}
      </a>
    </section>

    <!-- 6. Worktrees. -->
    {#if o.worktrees.length > 1}
      <section class="g-section" aria-label="Worktrees">
        <header>
          <h2>Worktrees</h2>
          <span class="count">{o.worktrees.length - 1} besides your checkout</span>
        </header>
        <ul class="g-list">
          {#each o.worktrees.filter((w) => !w.primary) as w (w.path)}
            <li class="card g-card wt">
              <div class="g-row">
                <strong class="g-wrap">{w.branch || 'detached'}</strong>
                {#if w.owned}<span class="g-chip" data-tone="work">Dev Board</span>{/if}
                {#if w.missing}<span class="g-chip" data-tone="bad">directory gone</span>{/if}
                {#if w.locked}<span class="g-chip">locked</span>{/if}
                {#if w.activeRun}<span class="g-chip" data-tone="work">agent working</span>{/if}
                {#if w.dirty && dirtyTotal(w.dirty) > 0}<span class="g-chip" data-tone="ask">{dirtyTotal(w.dirty)} uncommitted</span>{/if}
              </div>
              <p class="mono g-small muted g-wrap">{w.path}</p>
              <div class="g-row">
                {#if w.taskId}<a class="g-small" href={taskHref(project.id, w.taskId)}>{w.taskTitle || 'Task'}</a>{/if}
                {#if !w.missing}<a class="g-small" href={changesHref(project.id, w.worktreeId ?? '')}>Working changes</a>{/if}
                {#if w.owned && w.worktreeId && !w.activeRun}
                  <button
                    class="btn small danger"
                    onclick={() => sheets.open({ kind: 'clean', worktreeId: w.worktreeId ?? '', label: w.taskTitle || w.branch || w.path, branch: w.branch ?? '', head: w.head ?? '' })}
                  >Clean up…</button>
                {/if}
              </div>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <!-- 7. Every branch. -->
    <section class="g-section" aria-label="All branches">
      <header>
        <h2>Branches</h2>
        <span class="count">{listed.length}</span>
      </header>
      <div class="filters" role="tablist" aria-label="Filter branches">
        {#each BRANCH_FILTERS as f (f.id)}
          <button
            class="filter"
            role="tab"
            aria-selected={filter === f.id}
            onclick={() => {
              chosen = f.id;
              showAllBranches = false;
            }}
          >{f.label}</button>
        {/each}
      </div>
      {#if listed.length}
        <ul class="g-list">
          {#each showAllBranches ? listed : listed.slice(0, SHOW_BRANCHES) as b (b.ref)}
            <BranchCard projectId={project.id} branch={b} targetName={o.local.target.name} ctx={ctxFor(b.name)} />
          {/each}
        </ul>
        {#if listed.length > SHOW_BRANCHES && !showAllBranches}
          <button class="btn quiet" onclick={() => (showAllBranches = true)}>Show all {listed.length}</button>
        {/if}
      {:else}
        <p class="card empty">No branches here.</p>
      {/if}
    </section>
  </div>
{/if}

<style>
  .result {
    display: grid;
    gap: 6px;
    justify-items: start;
  }

  .result :global(.g-notice) {
    width: 100%;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 10px;
  }

  .title {
    font-size: 1.1rem;
    font-weight: 650;
  }

  .scopes {
    display: grid;
    gap: 10px;
  }

  @media (min-width: 640px) {
    .scopes {
      grid-template-columns: 1fr 1fr;
    }
  }

  .when {
    text-transform: none;
    letter-spacing: 0;
    font-weight: 500;
    margin-left: 6px;
  }

  .ok {
    color: var(--ok);
  }

  .sync {
    display: grid;
    gap: 4px;
    margin-top: 10px;
    font-size: 0.88rem;
  }

  .sync summary {
    cursor: pointer;
    min-height: 32px;
    display: flex;
    align-items: center;
  }

  .counts {
    margin-top: 10px;
  }

  .notes {
    margin-top: 8px;
  }

  .commits {
    overflow: hidden;
  }

  .changes {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    color: inherit;
    text-decoration: none;
    min-height: 48px;
  }

  .pr,
  .wt {
    display: grid;
    gap: 5px;
  }

  .prtitle {
    font-weight: 600;
  }

  .filters {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    padding-bottom: 2px;
    scrollbar-width: none;
  }

  .filter {
    flex: none;
    min-height: 36px;
    padding: 0 12px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--surface);
    font-size: 0.85rem;
    font-weight: 550;
    color: var(--text-2);
  }

  .filter[aria-selected='true'] {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-text);
  }
</style>
