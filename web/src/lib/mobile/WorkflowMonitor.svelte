<script lang="ts">
  import { onMount } from 'svelte';
  import { app } from '../state.svelte';
  import { router } from '../router.svelte';
  import { globalHref, taskHref } from '../location';
  import Sheet from '../Sheet.svelte';
  import Icon from '../Icon.svelte';
  import WorkflowBanner from './WorkflowBanner.svelte';
  import { workflow } from './layout';
  import { workflows } from './workflows.svelte';
  import { terminal, workflowSteps, workflowStatus, workflowRecovery } from './workflow-progress';
  const selected = $derived(workflows.executions.find(x => x.taskId === workflows.selected));
  const repositories = $derived(selected ? workflows.executions.filter(x => x.requestId === selected.requestId) : []);
  onMount(() => {
    void workflows.refresh();
    // History discovery only: visible stage progress always comes from durable events.
    const discover = setInterval(() => { if (document.visibilityState === 'visible' && app.connection === 'live') void workflows.refresh(false); }, 15000);
    const reconnect = app.onReconnect(() => void workflows.refresh());
    const visible = () => { if (document.visibilityState === 'visible') void workflows.refresh(); };
    document.addEventListener('visibilitychange', visible);
    return () => { clearInterval(discover); reconnect(); document.removeEventListener('visibilitychange', visible); workflows.dispose(); };
  });
</script>
{#if router.view !== 'deck'}<WorkflowBanner />{/if}
{#if selected}
  <Sheet title="{workflow(selected.configuration?.button ?? '')?.label ?? 'Workflow'} execution" onclose={() => workflows.selected = ''}>
    {@const steps = workflowSteps(selected.run, selected.events, selected.configuration)}
    <p role="status">{workflowStatus(selected.run)} · {app.project(selected.projectId)?.name ?? 'Repository'}</p>
    <p class="muted">{selected.run?.agentId} {selected.run?.model ?? ''}</p>
    {#if repositories.length > 1}
      <nav class="repositories" aria-label="Workflow repositories">
        {#each repositories as repository (repository.taskId)}<button class="btn" aria-current={repository.taskId === selected.taskId ? 'true' : undefined} onclick={() => workflows.open(repository.taskId)}>{app.project(repository.projectId)?.name ?? 'Repository'} · {workflowStatus(repository.run)}</button>{/each}
      </nav>
    {/if}
    {#if app.connection !== 'live'}<p role="status">Connection interrupted. Showing saved events; the authorised runner may still be working.</p>{/if}
    {#if selected.stopRequested}<p role="status">{selected.run?.state === 'stopped' ? 'Cancellation confirmed. Completed results are preserved.' : terminal(selected.run) ? 'The run ended before cancellation was confirmed. Results are preserved.' : 'Stop requested. Cancellation is not confirmed until the runner reports it stopped.'}</p>{/if}
    {#if selected.error || selected.run?.reason}<p class="error" role="alert">{selected.error || selected.run?.reason}</p><p>{workflowRecovery(selected.error || selected.run?.reason || '')}</p>{/if}
    {#if selected.run?.state === 'failed' || selected.run?.state === 'stopped'}<p>Partial completion: inspect completed stages and saved output before starting more work.</p>{/if}
    {#if selected.findings?.length}
      <section aria-label="Actionable findings" class="findings">
        <h3>Next actions</h3>
        <p class="muted">Review each finding’s evidence and suggested fix in its task. New findings wait in Backlog for you to assign or schedule.</p>
        <ul>{#each selected.findings as finding (finding.id)}
          <li><strong>{finding.title}</strong><span class="muted">{finding.archivedAt ? 'Archived' : finding.state === 'backlog' ? 'Backlog draft' : finding.state === 'doing' ? 'In progress' : finding.state === 'review' ? 'In review' : 'Done'}</span><a class="btn workflow-action" href={taskHref(selected.projectId, finding.id)} onclick={() => workflows.selected = ''}>Review finding<small class="visually-hidden">: {finding.title}</small></a></li>
        {/each}</ul>
      </section>
    {:else if selected.run?.state === 'completed' && selected.configuration?.output === 'drafts'}
      <p class="muted">No actionable draft tasks were saved. Read the report for findings and any limits on the investigation.</p>
    {/if}
    <p class="muted">{steps.filter(s => s.status === 'Completed').length} of {steps.length} stages reported complete. Investigation stages are agent-reported; stages without events stay pending.</p>
    <ol class="steps">
      {#each steps as step, i (step.name)}
        <li data-status={step.status}>
          <details open={step.status === 'Running' || step.status === 'Failed'}>
            <summary><span class="step-icon" class:running={step.status === 'Running'} aria-hidden="true"><Icon name={step.status === 'Completed' ? 'check' : step.status === 'Failed' ? 'close' : step.status === 'Cancelled' ? 'stop' : 'activity'} size={16} /></span><span>{i + 1}. {step.name}<small>{step.status}</small></span></summary>
            {#if step.output.length}<pre>{step.output.join('\n')}</pre>{:else}<p class="muted">No execution event reported for this stage.</p>{/if}
          </details>
        </li>
      {/each}
    </ol>
    {#snippet footer()}
      {#if repositories.some(x => x.run && !terminal(x.run))}<button class="btn workflow-action" disabled={selected.stopping} onclick={() => void workflows.stopWorkflow(selected.requestId)}>{selected.stopping ? 'Stopping…' : 'Stop workflow'}</button>{/if}
      <button class="btn workflow-action" onclick={() => void workflows.refresh()}>Refresh state</button>
      <a class="btn workflow-action" href={globalHref('settings')} onclick={() => workflows.selected = ''}>Change Settings</a>
      <a class="btn primary workflow-action" href={taskHref(selected.projectId, selected.taskId, selected.run?.id)} onclick={() => workflows.selected = ''}>Open task / retry</a>
    {/snippet}
  </Sheet>
{/if}
<style>
  .workflow-action { min-height: 44px; }
  .findings h3 { font-size: 16px; margin: 0; }
  .findings ul { list-style: none; padding: 0; margin: 0; }
  .findings li { display: flex; flex-wrap: wrap; gap: 8px 12px; align-items: center; }
  .findings strong { flex: 1 1 100%; font-weight: 500; }
  .findings a { margin-left: auto; }
  .repositories { display: flex; flex-wrap: wrap; gap: 8px; }
  .repositories .btn { min-height: 44px; }
  .steps { list-style: none; margin: 20px 0; padding: 0; }
  li { padding: 8px 0; border-bottom: 1px solid var(--border); }
  summary { display: flex; align-items: center; gap: 10px; min-height: 44px; cursor: pointer; font-weight: 500; }
  small { display: block; color: var(--text-2); font-size: 12px; margin-top: 3px; }
  .step-icon { color: var(--text-2); display: flex; }
  [data-status='Completed'] .step-icon { color: var(--ok-text); }
  [data-status='Failed'] .step-icon { color: var(--danger-text); }
  .running { color: var(--accent); animation: pulse 1.3s ease-in-out infinite alternate; }
  pre { max-height: 22rem; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 12px; padding: 10px; background: var(--surface-2); }
  @keyframes pulse { to { opacity: .4; } }
  @media (prefers-reduced-motion: reduce) { .running { animation: none; } }
</style>
