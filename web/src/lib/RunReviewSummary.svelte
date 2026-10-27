<script lang="ts">
  import { gitStore } from './git/store.svelte';
  import { branchHref, taskReviewBranchHref } from './gitroute';
  import { safeURL } from './gitui';
  import { sheets } from './git/sheets.svelte';
  import Icon from './Icon.svelte';
  import type { Run } from './types';
  import RunAssessment from './RunAssessment.svelte';

  let { run, onmerged }: { run: Run; onmerged?: () => void } = $props();
  const git = $derived(gitStore(run.projectId));
  const branch = $derived(!run.remote ? git.overview?.branches.find(b => b.scope === 'local' && (b.werkbord.runId === run.id || (!!run.branch && b.name === run.branch))) : undefined);
  const reviewHref = $derived(branch
    ? branch.werkbord.taskId === run.taskId && branch.werkbord.runId === run.id
      ? taskReviewBranchHref(run.projectId, branch.scope, branch.name, run.taskId, run.id)
      : branchHref(run.projectId, branch.scope, branch.name)
    : '');
  const canMerge = $derived(!!branch && !branch.merged && branch.vsTarget.ahead > 0);
  const pr = $derived(git.github?.pullRequests.find(p => p.runId === run.id));
  const prURL = $derived(pr ? safeURL(pr.url) : '#');
  const lookupFailed = $derived(!!git.error || (git.github?.reason === 'error' && !git.github.available));
</script>

<section class="review-lead" aria-label="Review changes">
  <div class="actions">
    {#if branch}
      <a class="btn primary" href={reviewHref}>Review changes</a>
    {:else if pr && prURL !== '#'}
      <a class="btn primary" href={prURL} target="_blank" rel="noopener noreferrer">Review pull request #{pr.number}</a>
    {:else}
      <p class="muted">{lookupFailed ? 'Git evidence could not be checked. Open full evidence for details.' : git.loading || git.githubLoading ? 'Checking available review evidence…' : run.remote ? 'Changes remain on the runner. No pull request is available here.' : 'No local diff or pull request is available here.'}</p>
    {/if}
    {#if canMerge}<button class="btn ok" onclick={() => sheets.open({ kind: 'merge', branch: branch!.name, onmerged })}><Icon name="merge" size={14} />Merge…</button>{/if}
  </div>
  <p class="summary">
    {#if branch}<span>Local branch <code>{branch.name}</code> · {branch.vsTarget.ahead} commit{branch.vsTarget.ahead === 1 ? '' : 's'} ahead</span>
    {:else if pr}<span>Pull request #{pr.number} · {pr.title}</span>
    {/if}
    <span>{run.handoff?.tests.length ? 'Agent-reported tests · not independently verified' : 'No test notes reported'}</span>
  </p>
  {#if run.endedAt}<RunAssessment {run} />{/if}
</section>

<style>
  .review-lead { display: grid; gap: 6px; min-width: 0; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .actions p { margin: 0; }
  .summary { display: flex; flex-wrap: wrap; gap: 4px 12px; margin: 0; color: var(--text-2); font-size: 12px; }
  .summary code { color: var(--text); overflow-wrap: anywhere; }
  :global(.review-lead .assessment) { padding-top: 2px; }
  @media (max-width: 899px) {
    .actions > :is(a, button) { min-height: 44px; }
  }
</style>
