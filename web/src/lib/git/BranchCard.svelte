<script lang="ts">
  import { timeAgo } from '../format';
  import { branchHref } from '../gitroute';
  import {
    branchChips,
    headline,
    originLabel,
    quickActions,
    relationLabel,
    shortSha,
    splitBranch,
    upstreamLabel,
    type ActionContext,
    type BranchAction,
  } from '../gitui';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { GitBranch } from '../types';
  import Chips from './Chips.svelte';
  import { sheets } from './sheets.svelte';

  // One branch as a card: what it is, how it stands, who made it, and the one or two
  // things you are likely to do next, a tap away. Everything else is one level down.

  let {
    projectId,
    branch,
    targetName,
    ctx,
    emphasis = false,
  }: { projectId: string; branch: GitBranch; targetName: string; ctx: ActionContext; emphasis?: boolean } = $props();

  const parts = $derived(splitBranch(branch.name));
  const chips = $derived(branchChips(branch, targetName));
  const quick = $derived(quickActions(branch, ctx));
  const why = $derived(headline(branch));
  const href = $derived(branchHref(projectId, branch.scope, branch.name));

  function act(a: BranchAction) {
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
    }
  }
</script>

<li class="card item" class:owned={branch.devboard.created} class:emphasis data-severity={why?.severity ?? 'none'}>
  <a class="main" {href}>
    <span class="name g-wrap"><span class="prefix">{parts.prefix}</span>{parts.rest}</span>
    <span class="g-row chips"><Chips {chips} /></span>
    {#if why}<span class="why g-small" data-severity={why.severity}>{why.message}</span>{/if}
    <span class="muted g-small g-wrap">
      {relationLabel(branch, targetName)}{#if branch.scope === 'local' && !branch.target} · {upstreamLabel(branch.upstream)}{/if}
    </span>
    <span class="muted g-small g-wrap">
      <span class="mono">{shortSha(branch.sha)}</span> {branch.subject} · {timeAgo(branch.commitDate, app.now)}
    </span>
  </a>

  <div class="foot">
    <span class="origin muted g-small g-wrap">
      {#if branch.devboard.taskId}
        <a href={taskHref(projectId, branch.devboard.taskId)}>{branch.devboard.taskTitle || 'Task'}</a>
      {:else}
        {originLabel(branch)}
      {/if}
    </span>
    <span class="buttons">
      <a class="btn small" {href}>Review</a>
      {#each quick as a (a.id)}
        <button class="btn small" class:danger={a.danger} onclick={() => act(a)}>{a.label}</button>
      {/each}
    </span>
  </div>
</li>

<style>
  .item {
    display: grid;
    min-width: 0;
    border-left: 3px solid var(--border);
  }

  .item.owned {
    border-left-color: var(--accent);
  }

  .item[data-severity='action'] {
    border-left-color: var(--warn);
  }

  .item[data-severity='warn'] {
    border-left-color: var(--block);
  }

  .item.emphasis {
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent) 35%, transparent);
  }

  .main {
    display: grid;
    gap: 4px;
    padding: 12px 14px 8px;
    color: inherit;
    text-decoration: none;
    min-width: 0;
  }

  .name {
    font-weight: 650;
  }

  .prefix {
    color: var(--text-2);
    font-weight: 500;
  }

  .why {
    font-weight: 600;
  }

  .why[data-severity='action'] {
    color: var(--warn);
  }

  .why[data-severity='warn'] {
    color: var(--block);
  }

  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 4px 14px 12px;
    min-width: 0;
  }

  .origin {
    flex: 1 1 8rem;
    min-width: 0;
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
</style>
