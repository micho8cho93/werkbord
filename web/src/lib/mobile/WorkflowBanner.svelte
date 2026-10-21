<script lang="ts">
  import { acknowledge, surface } from '../motion';
  import { app } from '../state.svelte';
  import Icon from '../Icon.svelte';
  import { workflow } from './layout';
  import { workflows } from './workflows.svelte';
  import { terminal, workflowSteps, workflowStatus } from './workflow-progress';
  let node = $state<HTMLElement>();
  let headerHeight = $state(0);
  $effect(() => {
    if (!node || node.closest('.deck-page')) return;
    const header = node.closest('.main-area')?.querySelector(':scope > header');
    if (!header) return;
    const measure = () => { headerHeight = header.getBoundingClientRect().height; };
    measure();
    const observer = new ResizeObserver(measure); observer.observe(header);
    return () => observer.disconnect();
  });
  const visible = $derived(workflows.executions.filter(x => !workflows.dismissed.includes(x.taskId) && (!terminal(x.run) || !!x.error || x.requestId === workflows.executions[0]?.requestId)));
</script>
{#if workflows.starting || visible.length || workflows.error}
  <section in:surface={{ duration: workflows.starting ? 180 : 0 }} bind:this={node} style:top={`${headerHeight}px`} class="workflow-banner" aria-label="Workflow progress" data-testid="workflow-banner">
    {#if workflows.starting}<p role="status">{workflows.starting.name} · Starting workflow…</p>{/if}
    {#if workflows.error}<p role="alert">Execution history could not be loaded. {workflows.error}</p><button class="btn" onclick={() => void workflows.refresh()}>Retry loading</button>{/if}
    {#if app.connection !== 'live'}<p role="status">Connection interrupted. The runner may still be working. Progress will refresh when reconnected.</p>{/if}
    {#each [...new Set(visible.map(x => x.requestId))] as requestId (requestId)}
      {@const active = visible.filter(x => x.requestId === requestId && !terminal(x.run))}
      {#if active.length > 1}<button class="btn stop" onclick={() => void workflows.stopWorkflow(requestId)}>Stop workflow in all {active.length} repositories</button>{/if}
    {/each}
    {#each visible as x (x.taskId)}
      {@const steps = workflowSteps(x.run, x.events, x.configuration)}
      {@const current = steps.find(s => s.status === 'Running' || s.status === 'Failed' || s.status === 'Waiting for approval')}
      <div class="execution" use:acknowledge={{ value: x.run?.state, success: x.caughtUp && x.run?.state === 'completed' }}>
        <button class="open" onclick={() => workflows.open(x.taskId)} aria-label="Open {workflow(x.configuration?.button ?? '')?.label ?? 'workflow'} execution details">
          <strong>{workflow(x.configuration?.button ?? '')?.label ?? 'Workflow'}</strong>
          <span role="status">{workflowStatus(x.run)}{x.stopRequested && !terminal(x.run) ? ' · Stop requested; awaiting runner confirmation' : ''}</span>
          <span>{current?.name ?? (terminal(x.run) ? 'Execution ended' : x.run ? 'Waiting for stage report' : 'Preparing run')} · {app.project(x.projectId)?.name ?? 'Repository'}</span>
          <span class="muted">{x.run?.agentId}{x.run?.model ? ` · ${x.run.model}` : ''} · {steps.filter(s => s.status === 'Completed').length} of {steps.length} stages reported complete</span>
        </button>
        {#if !terminal(x.run)}<button class="btn stop" disabled={!x.run || x.stopping} onclick={() => void workflows.stop(x)} aria-label="Stop {workflow(x.configuration?.button ?? '')?.label ?? 'workflow'} in {app.project(x.projectId)?.name ?? 'repository'}"><Icon name="stop" />{x.stopping ? 'Stopping…' : 'Stop'}</button>{:else}<button class="btn stop" onclick={() => workflows.dismissed = [...workflows.dismissed, x.taskId]} aria-label="Dismiss finished workflow"><Icon name="close" /></button>{/if}
      </div>
      {#if x.error}<p class="error" role="alert">{x.error}</p>{/if}
    {/each}
  </section>
{/if}
<style>
  .workflow-banner { position: sticky; z-index: 6; display: grid; gap: 8px; padding: 10px 14px; background: var(--surface); border-bottom: 1px solid var(--border); max-height: 34dvh; overflow-y: auto; }
  .execution { display: flex; gap: 8px; align-items: center; min-width: 0; }
  .open { flex: 1; display: grid; gap: 3px; text-align: left; padding: 4px; background: transparent; color: var(--text); border: 0; min-height: 44px; min-width: 0; cursor: pointer; }
  .open span { overflow-wrap: anywhere; font-size: 12px; }
  .open strong { font-size: 14px; }
  .stop { min-height: 44px; flex-shrink: 0; }
  p { margin: 0; font-size: 12px; }
</style>
