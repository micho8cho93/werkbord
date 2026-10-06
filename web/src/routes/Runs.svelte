<script lang="ts">
  import { agentLabel, optionLabel } from '../lib/execution';
  import { runElapsed, runStatus } from '../lib/format';
  import Mark from '../lib/Mark.svelte';
  import HistoryStrip from '../lib/HistoryStrip.svelte';
  import { interactionShort, isNotable } from '../lib/policy';
  import { taskHref } from '../lib/router.svelte';
  import { shortRunId, tokensShort } from '../lib/runs';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import type { Project, Run } from '../lib/types';

  // Every agent run in the project, newest first: who ran what, where, how it went and what it used.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  let stateFilter = $state<'' | 'active' | 'completed' | 'failed' | 'stopped'>('');
  let agentFilter = $state('');
  let runnerFilter = $state('');
  const filtering = $derived(!!(stateFilter || agentFilter || runnerFilter));

  const titleOf = (id: string) => scope.tasks.find((t) => t.id === id)?.title ?? 'Task';
  const runners = $derived([...new Set(scope.history.map((r) => r.runnerId ?? ''))]);
  const agents = $derived([...new Set(scope.history.map((r) => r.agentId))]);
  const runnerName = (id: string | undefined, remote?: boolean) => {
    const m = app.runners.find((x) => x.id === id);
    return m ? (m.kind === 'local' ? 'this computer' : m.name) : remote ? (id ?? 'remote') : 'this computer';
  };

  const rows = $derived(
    scope.history.filter((r) => {
      const s = runStatus(r);
      if (stateFilter === 'active' && !s.active) return false;
      if (stateFilter && stateFilter !== 'active' && r.state !== stateFilter) return false;
      if (agentFilter && r.agentId !== agentFilter) return false;
      if (runnerFilter && (r.runnerId ?? '') !== runnerFilter) return false;
      return true;
    }),
  );

  const cost = (r: Run) =>
    r.usage?.costUsd === undefined ? '' : `${r.usage.costKind === 'actual_api' ? '' : '≈'}${new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD' }).format(r.usage.costUsd)}`;
  const started = (iso: string) =>
    new Date(iso).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
</script>

<div class="runs">
  <div class="bar">
    <HistoryStrip runs={scope.history} />
    <div class="filters">
      <label class="pick" class:on={stateFilter}>
        <span>State</span>
        <select bind:value={stateFilter} aria-label="Filter by state">
          <option value="">any</option>
          <option value="active">active</option>
          <option value="completed">completed</option>
          <option value="failed">failed</option>
          <option value="stopped">stopped</option>
        </select>
      </label>
      {#if agents.length > 1}
        <label class="pick" class:on={agentFilter}>
          <span>Agent</span>
          <select bind:value={agentFilter} aria-label="Filter by agent">
            <option value="">any</option>
            {#each agents as a (a)}<option value={a}>{agentLabel(app.agents, a)}</option>{/each}
          </select>
        </label>
      {/if}
      {#if runners.length > 1}
        <label class="pick" class:on={runnerFilter}>
          <span>Runner</span>
          <select bind:value={runnerFilter} aria-label="Filter by runner">
            <option value="">any</option>
            {#each runners as r (r)}<option value={r}>{runnerName(r)}</option>{/each}
          </select>
        </label>
      {/if}
      {#if filtering}<button class="btn quiet small" onclick={() => ((stateFilter = ''), (agentFilter = ''), (runnerFilter = ''))}>Clear</button>{/if}
      <span class="mm count">{rows.length} of {scope.history.length}</span>
    </div>
  </div>

  <div class="table-area">
    {#if rows.length}
      <div class="tbl" role="table" aria-label="Runs in {project.name}">
        <div class="tr th" role="row">
          <span role="columnheader">Run</span>
          <span role="columnheader">Task</span>
          <span role="columnheader">Agent</span>
          <span role="columnheader">Runner</span>
          <span role="columnheader">State</span>
          <span role="columnheader">Started</span>
          <span role="columnheader" class="num-col">Time</span>
          <span role="columnheader" class="num-col">Tokens</span>
          <span role="columnheader" class="num-col">Cost</span>
        </div>
        {#each rows as r (r.id)}
          {@const s = runStatus(r)}
          <a class="tr" role="row" href={taskHref(project.id, r.taskId)} title={r.reason || s.hint}>
            <span class="mm" role="cell">{shortRunId(r.id)}{r.attempt && r.attempt > 1 ? ` · #${r.attempt}` : ''}</span>
            <span class="task" role="cell">
              {titleOf(r.taskId)}
              {#if r.purpose && r.purpose !== 'implement'}<span class="chip">{r.purpose}</span>{/if}
              {#if isNotable(r.policy)}<span class="chip">{interactionShort(r.policy)}</span>{/if}
            </span>
            <span role="cell">{agentLabel(app.agents, r.agentId)}{r.model ? ` · ${optionLabel(app.agentOptions.get(r.agentId)?.models, r.model)}` : ''}</span>
            <span class="mm" role="cell">{runnerName(r.runnerId, r.remote)}</span>
            <span class="st" role="cell"><span class="dot" data-tone={s.tone}></span>{s.label}</span>
            <span class="mm" role="cell">{started(r.createdAt)}</span>
            <span class="mm num-col" role="cell">{runElapsed(r, app.now)}</span>
            <span class="mm num-col" role="cell">{tokensShort(r) || '—'}</span>
            <span class="mm num-col" role="cell">{cost(r) || '—'}</span>
          </a>
        {/each}
      </div>
    {:else}
      <p class="empty">{!scope.loaded ? 'Loading…' : filtering ? 'No run matches the filters.' : 'No agent has run in this project yet. Start one from the Board.'}</p>
    {/if}
  </div>
  <div class="brand-canvas" aria-hidden="true"><Mark /><span>werkbord</span><p>Werk local, ship global.</p></div>
</div>

<style>
  .runs {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .bar {
    flex: none;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px 20px;
    padding: 16px 24px 12px;
  }

  .filters {
    margin-left: auto;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }

  .count {
    margin-left: 4px;
  }

  .table-area {
    flex: 0 1 auto;
    max-height: calc(100% - 180px);
    min-height: 0;
    overflow: auto;
    margin: 0 24px 24px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-tray);
    box-shadow: var(--card-sh);
  }

  .brand-canvas { flex: 1; min-height: 100px; display: grid; place-content: center; grid-template-columns: auto auto; align-items: center; gap: 8px 16px; color: var(--text-2); padding: 24px; }
  .brand-canvas span { font: 600 24px var(--mono); letter-spacing: -0.04em; }
  .brand-canvas p { grid-column: 1 / -1; text-align: center; margin: 0; font-size: 12px; }
  .brand-canvas :global(svg) { width: 60px; height: auto; }

  .tbl {
    min-width: 900px;
  }

  .tr {
    display: grid;
    grid-template-columns: 96px minmax(0, 2fr) minmax(0, 1.1fr) minmax(0, 0.9fr) 130px 120px 70px 64px 72px;
    gap: 12px;
    align-items: center;
    min-height: 40px;
    padding: 0 16px;
    border-top: 1px solid var(--border);
    font-size: 13px;
    color: var(--text);
    text-decoration: none;
  }

  a.tr:hover {
    background: var(--bg);
  }

  .tr > * {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .th {
    position: sticky;
    top: 0;
    z-index: 1;
    min-height: 34px;
    border-top: 0;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    font-family: var(--mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  .th + .tr {
    border-top: 0;
  }

  .task {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 500;
  }

  .st {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12.5px;
  }

  .num-col {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  @media (max-width: 1180px) {
    .runs {
      height: auto;
    }

    .bar {
      padding: 4px 16px 12px;
    }

    .filters {
      margin-left: 0;
    }

    .brand-canvas { display: none; }
    .table-area {
      max-height: none;
      margin: 0 16px 16px;
    }

    /* A phone reads a run as two lines: what and how it went, then who and how long. */
    .tbl {
      min-width: 0;
    }

    .th {
      display: none;
    }

    .tr {
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 2px 12px;
      padding: 10px 14px;
    }

    .tr > :nth-child(1),
    .tr > :nth-child(4),
    .tr > :nth-child(6),
    .tr > :nth-child(8),
    .tr > :nth-child(9) {
      display: none;
    }

    .tr > :nth-child(3) {
      font-size: 12px;
      color: var(--text-2);
    }
  }
</style>
