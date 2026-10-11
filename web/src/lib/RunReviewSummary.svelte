<script lang="ts">
  import { gitStore } from './git/store.svelte';
  import { branchHref } from './gitroute';
  import { safeURL } from './gitui';
  import { sheets } from './git/sheets.svelte';
  import Icon from './Icon.svelte';
  import type { Run } from './types';

  let { run, onmerged }: { run: Run; onmerged?: () => void } = $props();
  const git = $derived(gitStore(run.projectId));
  const branch = $derived(!run.remote ? git.overview?.branches.find(b => b.scope === 'local' && (b.devboard.runId === run.id || (!!run.branch && b.name === run.branch))) : undefined);
  const canMerge = $derived(!!branch && !branch.merged && branch.vsTarget.ahead > 0);
  const pr = $derived(git.github?.pullRequests.find(p => p.runId === run.id));
  const prURL = $derived(pr ? safeURL(pr.url) : '#');
  const lookupFailed = $derived(!!git.error || (git.github?.reason === 'error' && !git.github.available));
</script>

<section class="review-lead" aria-label="Review changes">
  <div class="actions">
    {#if branch}
      <a class="btn primary" href={branchHref(run.projectId, branch.scope, branch.name)}>Review changes</a>
    {:else if pr && prURL !== '#'}
      <a class="btn primary" href={prURL} target="_blank" rel="noopener noreferrer">Review pull request #{pr.number}</a>
    {:else}
      <p class="muted">{lookupFailed ? 'Git evidence could not be checked. Open full evidence for details.' : git.loading || git.githubLoading ? 'Checking available review evidence…' : run.remote ? 'Changes remain on the runner. No pull request is available here.' : 'No local diff or pull request is available here.'}</p>
    {/if}
    {#if canMerge}<button class="btn" onclick={() => sheets.open({ kind: 'merge', branch: branch!.name, onmerged })}><Icon name="merge" size={14} />Merge…</button>{/if}
  </div>
  <p class="summary">
    {#if branch}<span>Local branch <code>{branch.name}</code> · {branch.vsTarget.ahead} commit{branch.vsTarget.ahead === 1 ? '' : 's'} ahead</span>
    {:else if pr}<span>Pull request #{pr.number} · {pr.title}</span>
    {:else}<span>{lookupFailed ? 'Git evidence could not be checked' : git.loading || git.githubLoading ? 'Checking available review evidence…' : run.remote ? 'Branch retained on its runner' : 'No branch or pull request available'}</span>{/if}
    <span>Tests reported: {run.handoff?.tests.length ? `${run.handoff.tests.length} note${run.handoff.tests.length === 1 ? '' : 's'}` : 'none'} · not verified</span>
  </p>
</section>

<style>
  .review-lead { display: grid; gap: 6px; min-width: 0; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .actions p { margin: 0; }
  .summary { display: flex; flex-wrap: wrap; gap: 4px 12px; margin: 0; color: var(--text-2); font-size: 12px; }
  .summary code { color: var(--text); overflow-wrap: anywhere; }
  @media (max-width: 899px) {
    .actions > :is(a, button) { min-height: 44px; }
  }
</style>
