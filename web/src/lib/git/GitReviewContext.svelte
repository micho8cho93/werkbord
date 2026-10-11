<script lang="ts">
  import { api } from '../api';
  import { resolveGitReviewContext, type GitReviewContext as ReviewContext } from '../gitreviewcontext';
  import { taskHref } from '../router.svelte';
  import type { BranchScope, GitBranch } from '../types';

  let { projectId, scope, name, taskId, runId, branches: currentBranches }: {
    projectId: string;
    scope: BranchScope;
    name: string;
    taskId: string;
    runId: string;
    branches?: GitBranch[];
  } = $props();
  let context = $state<ReviewContext>();
  let requestId = 0;

  $effect(() => {
    const current = currentBranches?.find((b) => b.scope === scope && b.name === name);
    const currentSha = current?.sha;
    const currentStale = current?.stale;
    const currentTaskId = current?.devboard.taskId;
    const currentTaskTitle = current?.devboard.taskTitle;
    const currentRunId = current?.devboard.runId;
    const currentRunState = current?.devboard.runState;
    const currentTaskState = current?.devboard.taskState;
    const currentActiveRun = current?.devboard.activeRun;
    const id = ++requestId;
    context = undefined;
    if (!taskId || !runId || scope !== 'local' || !current || !currentSha || currentStale || currentTaskId !== taskId || !currentTaskTitle || currentRunId !== runId || !currentRunState || currentTaskState !== 'review' || currentActiveRun) return;
    Promise.all([api.listTasks(projectId), api.getRun(projectId, runId), api.git.overview(projectId)]).then(([tasks, run, overview]) => {
      if (id !== requestId) return;
      const branch = overview.branches.find((b) => b.scope === scope && b.name === name);
      if (!branch) return;
      context = resolveGitReviewContext({ projectId, taskId, runId, branch, branches: overview.branches, tasks, run });
      if (currentBranches) {
        const current = currentBranches.find((b) => b.scope === scope && b.name === name);
        if (!current || current.sha !== currentSha || current.sha !== branch.sha || current.stale || current.devboard.taskId !== taskId || current.devboard.taskTitle !== currentTaskTitle || current.devboard.runId !== runId || current.devboard.runState !== currentRunState || current.devboard.runState !== run.state || current.devboard.taskState !== 'review' || current.devboard.activeRun) context = undefined;
      }
    }).catch(() => {
      if (id === requestId) context = undefined;
    });
  });
</script>

{#if context}
  <section class="card g-card review-context" aria-label="Review from task">
    <a class="g-back" href={taskHref(projectId, context.task.id, context.run.id)}>← Back to {context.task.title}</a>
    <p class="result">{context.result}</p>
    <p class="tests"><strong>Tests reported</strong> · {context.tests.length ? `${context.tests.length} note${context.tests.length === 1 ? '' : 's'} · not independently verified` : 'none · not verified'}</p>
    {#if context.tests.length}<ul class="test-notes">{#each context.tests as test (test)}<li>{test}</li>{/each}</ul>{/if}
    <a class="btn quiet small" href={taskHref(projectId, context.task.id, context.run.id)}>Full run and Git evidence</a>
  </section>
{/if}

<style>
  .review-context { display: grid; justify-items: start; gap: 8px; }
  .review-context p { margin: 0; }
  .result { font-weight: 500; }
  .tests,
  .test-notes { color: var(--text-2); font-size: 12.5px; }
  .test-notes { margin: 0; padding-left: 1.2rem; }
</style>
