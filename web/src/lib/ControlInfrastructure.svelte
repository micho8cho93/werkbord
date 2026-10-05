<script lang="ts">
  import { globalHref, taskHref } from './router.svelte';
  import { app } from './state.svelte';

  // The machines work runs on, and what the work has cost, narrowed by the Control Center's filters.

  let { project = '', runner = '', agent = '' }: { project?: string; runner?: string; agent?: string } = $props();
  const machines = $derived(
    (app.overview?.runners ?? []).filter(
      (r) => (!runner || r.id === runner) && (!project || r.kind === 'local' || r.projects.includes(project)) && (!agent || r.capabilities.agents.some((a) => a.id === agent)),
    ),
  );
  const usage = $derived((app.overview?.usage ?? []).filter((u) => (!project || u.projectId === project) && (!runner || u.runnerId === runner) && (!agent || u.agentId === agent)));
  const usd = (n: number) => new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(n);
  const hours = (s: number) => (s < 3600 ? `${Math.round(s / 60)} min` : `${(s / 3600).toFixed(1)} h`);
  const tokens = (n: number) => (n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1000 ? `${Math.round(n / 1000)}k` : String(n));
</script>

<section class="pn" aria-labelledby="infra-title">
  <div class="ph"><span id="infra-title">Runners</span><span class="chip">{machines.length}</span><a class="end small" href="{globalHref('settings')}?runners">Manage</a></div>
  {#if !machines.length}<p class="muted small">No runners match this view.</p>{/if}
  <ul class="rows">
    {#each machines as r (r.id)}
      {@const live = r.online && !r.disabled}
      <li>
        <div class="line">
          <span class="dot" data-tone={live ? 'ok' : 'neutral'}></span>
          <strong>{r.kind === 'local' ? `${r.name} · this computer` : r.name}</strong>
          <span class="mm end">{r.disabled ? 'disabled' : r.online ? `${r.currentRuns}/${r.capacity}` : 'offline'}</span>
        </div>
        <p class="mm sub">{r.os}/{r.arch} · {(r.capabilities?.agents ?? []).filter((a) => a.available).map((a) => a.name).join(', ') || 'no agents available'}</p>
        {#each (app.overview?.runs ?? []).filter((x) => x.run.runnerId === r.id && (!project || x.run.projectId === project) && (!agent || x.run.agentId === agent)) as item (item.run.id)}
          <a class="work" href={taskHref(item.run.projectId, item.run.taskId)}>{item.taskTitle}<span class="mm">{item.run.state.replaceAll('_', ' ')}</span></a>
        {/each}
        {#if !r.online && r.currentRuns > 0}<p class="warning">Keeps ownership of its work. Reconnect it to reconcile.</p>{/if}
      </li>
    {/each}
  </ul>
</section>

<section class="pn" aria-labelledby="usage-title">
  <div class="ph"><span id="usage-title">Usage</span><span class="mm end">past 30 days</span></div>
  {#if !usage.length}
    <p class="muted small">No usage in this view yet.</p>
  {:else}
    <ul class="rows">
      {#each usage as u (`${u.projectId}/${u.runnerId}/${u.agentId}`)}
        <li>
          <div class="line">
            <strong>{app.project(u.projectId)?.name ?? u.projectId}</strong>
            <span class="mm">{app.agents.find((a) => a.id === u.agentId)?.name ?? u.agentId}</span>
            <span class="mm end">{u.runs} {u.runs === 1 ? 'run' : 'runs'}</span>
          </div>
          <dl class="nums">
            <div><dt>Time</dt><dd>{hours(u.runtimeSeconds)}</dd></div>
            <div><dt>Tokens</dt><dd>{u.tokenRuns ? tokens(u.inputTokens + u.outputTokens) : '—'}</dd></div>
            <div>
              <dt>{u.actualCostRuns ? 'Cost' : 'Est. cost'}</dt>
              <dd>{u.actualCostRuns ? usd(u.actualCostUsd) : u.estimatedCostRuns ? usd(u.estimatedCostUsd) : '—'}</dd>
            </div>
            <div><dt>Kept</dt><dd>{u.accepted}/{u.accepted + u.rejected || 0}</dd></div>
          </dl>
          <p class="mm sub">
            {app.runners.find((r) => r.id === u.runnerId)?.name ?? 'earlier runner'} · {u.retries} retries · tokens from {u.tokenRuns}/{u.runs}, cost from {u.costRuns}/{u.runs} runs
          </p>
        </li>
      {/each}
    </ul>
    <p class="mm">Only what agents reported. An estimate is not a bill.</p>
  {/if}
</section>

<style>
  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  .rows li {
    display: grid;
    gap: 4px;
    padding: 10px 0;
    border-top: 1px solid var(--border);
  }

  .rows li:first-child {
    border-top: 0;
    padding-top: 0;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    min-width: 0;
  }

  .line strong {
    font-weight: 500;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .end {
    margin-left: auto;
  }

  a.end {
    font-weight: 500;
    font-size: 12px;
    text-decoration: none;
  }

  .sub {
    overflow-wrap: anywhere;
  }

  .work {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    font-size: 12.5px;
    color: var(--text);
    text-decoration: none;
  }

  .work:hover {
    text-decoration: underline;
  }

  .warning {
    font-size: 12px;
    color: var(--warn-text);
  }

  .small {
    font-size: 12px;
  }

  .nums {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 8px;
    margin: 2px 0 0;
  }

  .nums div {
    display: grid;
    gap: 1px;
  }

  .nums dt {
    font-family: var(--mono);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  .nums dd {
    margin: 0;
    font-size: 13px;
    font-variant-numeric: tabular-nums;
  }
</style>
