<script lang="ts">
  import { ApiError, api } from '../api';
  import { timeAgo } from '../format';
  import {
    actionPlan,
    basisHint,
    basisLabel,
    detectedLabel,
    isDestructive,
    severityLabel,
    severityTone,
  } from '../health';
  import { router, taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { HealthFinding } from '../types';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  // One finding: what is wrong, why, what it rests on, and the next step. The step is only ever
  // offered, never taken: a button opens the same confirmation as anywhere else in the Git screen
  // (which checks again when confirmed); a step Dev Board cannot do says so and says what to do.

  let {
    finding,
    projectId,
    store,
    compact = false,
  }: { finding: HealthFinding; projectId: string; store: GitStore; compact?: boolean } = $props();

  const plan = $derived(actionPlan(finding, projectId));
  const dismissed = $derived(finding.state === 'dismissed');
  let busy = $state(false);
  let error = $state('');

  async function run(fn: () => Promise<void>) {
    busy = true;
    error = '';
    try {
      await fn();
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  function act() {
    const p = plan;
    switch (p.type) {
      case 'sheet':
        sheets.open({ kind: p.sheet, branch: p.branch });
        break;
      case 'clean': {
        // The clean sheet is pinned to the worktree's commit as the user sees it now.
        const o = store.overview;
        const b = o?.branches.find((x) => x.scope === 'local' && x.name === p.branch);
        const w = o?.worktrees.find((x) => x.worktreeId === p.worktreeId);
        sheets.open({ kind: 'clean', worktreeId: p.worktreeId, label: finding.subject.taskTitle || p.branch, branch: p.branch, head: w?.head || b?.sha || '' });
        break;
      }
      case 'fetch':
        void run(async () => {
          store.lastResult = await api.git.fetch(projectId);
          await store.reloadAll();
        });
        break;
      case 'task':
        // A card on the board, prefilled. Nothing is started: an agent runs only when you press Run.
        void run(async () => {
          const t = await api.createTask(projectId, p.title, p.description);
          router.go({ view: 'task', projectId, taskId: t.id });
        });
        break;
      default:
        break;
    }
  }
</script>

<li class="card finding" data-severity={finding.severity} class:compact class:dismissed>
  <header>
    <span class="g-chip" data-tone={severityTone(finding.severity)}>{severityLabel(finding.severity)}</span>
    <span class="g-chip basis" data-tone="neutral" title={basisHint(finding.basis)}>{basisLabel(finding.basis)}</span>
    {#if finding.subject.taskId}
      <a class="task g-small" href={taskHref(projectId, finding.subject.taskId)}>{finding.subject.taskTitle || 'Task'}</a>
    {/if}
  </header>

  <h3 class="title g-wrap">{finding.title}</h3>
  {#if !compact}<p class="why g-wrap">{finding.explanation}</p>{/if}

  {#if finding.evidence.length && !compact}
    <details class="evidence">
      <summary class="g-small">Evidence</summary>
      <dl class="g-kv">
        {#each finding.evidence as e (e.label + e.value)}
          <dt>{e.label}</dt>
          <dd class="g-wrap">{e.value}</dd>
        {/each}
      </dl>
      <p class="g-small muted">{basisHint(finding.basis)}</p>
    </details>
  {/if}

  <div class="next">
    {#if plan.type === 'manual'}
      <!-- Dev Board cannot do this one: say what to do, and why it is not a button. -->
      <div class="manual g-small">
        <p class="g-wrap"><strong>{finding.action.label}</strong></p>
        {#if plan.detail}<p class="g-wrap">{plan.detail}</p>{/if}
        <p class="muted g-wrap">Not something Dev Board does for you: {plan.reason}.</p>
      </div>
    {:else if plan.type === 'link'}
      <a class="btn small" class:primary={finding.severity !== 'info'} href={plan.href}>{finding.action.label}</a>
    {:else}
      <button class="btn small" class:danger={isDestructive(finding)} class:primary={finding.severity !== 'info'} disabled={busy} onclick={act} title={finding.action.detail}>
        {busy ? 'Working…' : finding.action.label + (plan.type === 'sheet' || plan.type === 'clean' ? '…' : '')}
      </button>
      {#if finding.action.detail}<span class="g-small muted g-wrap hint">{finding.action.detail}</span>{/if}
    {/if}
  </div>
  <div class="foot">
    <span class="seen g-small muted">{detectedLabel(finding, app.now, timeAgo)}</span>
    {#if dismissed}
      <button class="btn small quiet" disabled={busy} onclick={() => run(() => store.reopenFinding(finding.id))}>Show again</button>
    {:else}
      <button class="btn small quiet" disabled={busy} title="Hide this until it gets worse, or goes away and comes back. Changes nothing in the repository." onclick={() => run(() => store.dismissFinding(finding.id))}>I know</button>
    {/if}
  </div>
  {#if error}<p class="error g-small" role="alert">{error}</p>{/if}
</li>

<style>
  .finding {
    display: grid;
    gap: 6px;
    padding: 12px 14px;
    min-width: 0;
    border-left: 4px solid var(--border);
  }

  .finding[data-severity='attention'] {
    border-left-color: var(--warn);
  }

  .finding[data-severity='risk'] {
    border-left-color: var(--block);
    background: color-mix(in srgb, var(--block) 5%, var(--surface));
  }

  .finding[data-severity='critical'] {
    border-left-color: var(--danger);
    background: color-mix(in srgb, var(--danger) 7%, var(--surface));
  }

  .finding.dismissed {
    opacity: 0.75;
  }

  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 8px;
  }

  .task {
    margin-left: auto;
    min-width: 0;
  }

  .title {
    font-size: 0.98rem;
    font-weight: 650;
    margin: 0;
  }

  .compact .title {
    font-size: 0.9rem;
    font-weight: 600;
  }

  .why {
    margin: 0;
    color: var(--text-2);
    font-size: 0.9rem;
  }

  .evidence summary {
    cursor: pointer;
    color: var(--text-2);
    padding: 4px 0;
  }

  .next {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 10px;
    min-width: 0;
  }

  .manual {
    flex: 1 1 100%;
    min-width: 0;
    display: grid;
    gap: 3px;
    padding: 8px 10px;
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
  }

  .manual p {
    margin: 0;
  }

  .hint {
    flex: 1 1 10rem;
  }

  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-width: 0;
  }

  .seen {
    margin: 0;
  }
</style>
