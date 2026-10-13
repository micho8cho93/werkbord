<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { api } from '../lib/api';
  import { analyticsDuration, measuredCost, measuredNumber, type AnalyticsReport } from '../lib/analytics';
  import { agentLabel } from '../lib/execution';
  import { app } from '../lib/state.svelte';
  import type { RunState } from '../lib/types';

  let projectFilter = $state('');
  let days = $state(30);
  let provider = $state('');
  let report = $state<AnalyticsReport | null>(null);
  let loading = $state(true);
  let error = $state('');
  let requestId = 0;
  let mounted = true;
  onMount(() => {
    const timer = setInterval(() => { if (!document.hidden) void load(); }, 15000);
    return () => { mounted = false; clearInterval(timer); };
  });
  $effect(() => {
    void [projectFilter, days, provider, app.analyticsRevision];
    const timer = setTimeout(() => untrack(() => void load()), 120);
    return () => clearTimeout(timer);
  });

  async function load() {
    const id = ++requestId;
    loading = true; error = '';
    try {
      const next = await api.analytics(projectFilter, days, provider);
      if (mounted && id === requestId) report = next;
    } catch (e) {
      if (mounted && id === requestId) error = e instanceof Error ? e.message : 'Could not load analytics.';
    } finally {
      if (mounted && id === requestId) loading = false;
    }
  }
  const projectName = (id: string) => app.project(id)?.name ?? id;
  const runnerName = (id: string) => app.runners.find((r) => r.id === id)?.name ?? (id || 'Not recorded');
  const maxDaily = $derived(Math.max(1, ...(report?.days.flatMap((d) => [d.created, d.completed]) ?? [])));
  const outcomes: [RunState, string][] = [['completed', 'Normal exit'], ['failed', 'Failed'], ['stopped', 'Stopped'], ['blocked', 'Blocked'], ['waiting_for_user', 'Waiting'], ['starting', 'Starting'], ['running', 'Running']];
  let availableProviders = $state<string[]>([]);
  $effect(() => { if (report) availableProviders = report.providers; });
</script>

<div class="analytics">
  <div class="filters" aria-busy={loading}>
    <label class="pick"><span>Project</span><select bind:value={projectFilter} aria-label="Analytics project"><option value="">All projects</option>{#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select></label>
    <label class="pick"><span>Time range</span><select bind:value={days} aria-label="Analytics time range"><option value={7}>Last 7 days</option><option value={30}>Last 30 days</option><option value={90}>Last 90 days</option></select></label>
    <label class="pick"><span>Provider</span><select bind:value={provider} aria-label="Analytics agent provider"><option value="">All agent providers</option>{#each availableProviders as p (p)}<option value={p}>{agentLabel(app.agents, p)}</option>{/each}</select></label>
    {#if loading}<span class="muted" role="status">Updating…</span>{/if}
  </div>
  <p class="muted intro">Task throughput and agent execution. Provider means the agent runtime; its billing provider may be unknown.</p>
  {#if error}<p role="alert" class="error">{error} <button class="btn small" onclick={() => void load()}>Try again</button></p>
  {:else if loading && !report}<p class="empty" role="status">Loading analytics…</p>
  {:else if report}
    <p class="muted stamp">Recorded through {new Date(report.end).toLocaleString()}. Current blocked and stopped counts cover all ages; other counts use the selected period.</p>
    {#if report.truncated || report.retentionRemoved || report.missingTaskHistory}
      <p class="notice" role="status">Partial history. {report.truncated ? 'The bounded read omitted older records. ' : ''}{report.retentionRemoved ? 'Event retention has removed some evidence. ' : ''}{report.missingTaskHistory ? `${report.missingTaskHistory} Done records lack a known preceding state and were excluded. ` : ''}Counts may be lower than the full history.</p>
    {/if}
    <dl class="statistics">
      <div><dt>Tasks created</dt><dd>{report.created}</dd></div>
      <div><dt>Moved to Done</dt><dd>{report.completed}</dd></div>
      <div><dt>Average task duration</dt><dd>{analyticsDuration(report.averageTaskSeconds)}</dd></div>
      <div><dt>Median task duration</dt><dd>{analyticsDuration(report.medianTaskSeconds)}</dd></div>
      <div><dt>Currently blocked</dt><dd>{report.blocked}</dd></div>
      <div><dt>Currently stopped</dt><dd>{report.stopped}</dd></div>
    </dl>
    <p class="muted">Duration is elapsed time from task creation to its first recorded move to Done in this period, including waiting · {report.durationSamples} samples.</p>

    <section aria-labelledby="throughput-title">
      <h2 id="throughput-title">Created and completed over time</h2>
      <p class="legend"><span class="created-key"></span>Created <span class="completed-key"></span>Moved to Done · UTC days</p>
      <div class="chart-scroll"><div class="chart" role="img" aria-label="Daily task counts. Exact values are in the table below.">
        {#each report.days as day (day.date)}
          <div class="day" title="{day.date}: {day.created} created, {day.completed} moved to Done">
            <div class="bars"><span class="created-bar" style:height={`${day.created / maxDaily * 100}%`}></span><span class="completed-bar" style:height={`${day.completed / maxDaily * 100}%`}></span></div>
            <span class="mm">{day.date.slice(5)}</span>
          </div>
        {/each}
      </div></div>
      <details><summary>Daily values</summary><div class="table-scroll"><table><thead><tr><th>UTC date</th><th>Created</th><th>Moved to Done</th></tr></thead><tbody>{#each report.days as day (day.date)}<tr><td class="mm">{day.date}</td><td class="mm">{day.created}</td><td class="mm">{day.completed}</td></tr>{/each}</tbody></table></div></details>
    </section>

    <div class="breakdowns">
      <section aria-labelledby="completion-title"><h2 id="completion-title">Completion evidence</h2>
        <dl class="facts">
          <div><dt>Agent-reported task completion</dt><dd>{report.agentReportedCompletion ?? 'Not recorded'}</dd></div>
          <div><dt>Normal session ends</dt><dd>{report.normalSessionCompletions}</dd></div>
          <div><dt>Independently verified task completion</dt><dd>{report.verifiedCompletion ?? 'Not recorded'}</dd></div>
          <div><dt>Accepted / rejected</dt><dd>{report.accepted} / {report.rejected}</dd></div>
          <div><dt>Controller-verified merges</dt><dd>{report.verifiedMerges}</dd></div>
        </dl>
        <p class="muted">The controller records a normal session end; this is not an agent claim that its task is complete. Done is a board decision. Acceptance is an explicit run assessment. A verified merge confirms Git containment, with correctness and tests unverified.</p>
      </section>
      <section aria-labelledby="attempts-title"><h2 id="attempts-title">Execution attempts</h2>
        <p class="attempt-count mm">{report.attempts} attempts · {report.retries} retries</p>
        <dl class="facts">{#each outcomes as [state, label] (state)}<div><dt>{label}</dt><dd>{report.outcomes[state] ?? 0}</dd></div>{/each}</dl>
        <p class="muted">Current outcomes for runs started in the period. A retry is a recorded attempt number greater than one.</p>
      </section>
      <section aria-labelledby="usage-title"><h2 id="usage-title">Reported usage</h2>
        <dl class="facts">
          <div><dt>Input tokens</dt><dd>{measuredNumber(report.inputTokens)} <small>({report.inputRuns}/{report.attempts} runs)</small></dd></div>
          <div><dt>Output tokens</dt><dd>{measuredNumber(report.outputTokens)} <small>({report.outputRuns}/{report.attempts} runs)</small></dd></div>
          <div><dt>Cached tokens</dt><dd>{measuredNumber(report.cachedTokens)} <small>({report.cachedRuns}/{report.attempts} runs)</small></dd></div>
          <div><dt>Reported API cost</dt><dd>{measuredCost(report.actualCostUsd)} <small>({report.actualCostRuns}/{report.attempts} runs)</small></dd></div>
          <div><dt>Estimated API equivalent</dt><dd>{measuredCost(report.estimatedCostUsd)} <small>({report.estimatedCostRuns}/{report.attempts} runs)</small></dd></div>
          <div><dt>Detected usage-limit notices</dt><dd>{report.limitEvents || 'None detected'}</dd></div>
        </dl>
        <p class="muted">Lifetime counters for runs started in the period · {report.partialRuns} partial reports. Estimates require a recorded source and basis. Subscription charges and remaining quota are unknown; undetected notices may be missing.</p>
      </section>
    </div>

    <section aria-labelledby="execution-title"><h2 id="execution-title">Runner, provider and model</h2>
      {#if report.executions.length}<!-- svelte-ignore a11y_no_noninteractive_tabindex (The breakdown needs a keyboard-focusable horizontal scroll region.) -->
        <div class="table-scroll" role="region" aria-label="Runner, provider and model execution breakdown" tabindex="0"><table class="execution-table"><thead><tr>{#each ['Project', 'Runner', 'Agent provider', 'Configured model', 'Attempts / retries', 'Input / output tokens', 'API cost / estimated', 'Limit notices'] as label (label)}<th>{label}</th>{/each}</tr></thead>
        <tbody>{#each report.executions as row (`${row.projectId}/${row.runnerId}/${row.provider}/${row.model}`)}<tr>
          <td>{projectName(row.projectId)}</td><td>{runnerName(row.runnerId)}</td><td>{agentLabel(app.agents, row.provider)}</td><td>{row.model || 'Agent default · unknown'}</td>
          <td class="mm">{row.attempts} / {row.retries}</td><td class="mm">{measuredNumber(row.inputTokens)} / {measuredNumber(row.outputTokens)}<small>{row.inputRuns} input, {row.outputRuns} output reports</small></td>
          <td class="mm">{measuredCost(row.actualCostUsd)} / {measuredCost(row.estimatedCostUsd)}<small>{row.actualCostRuns} actual, {row.estimatedCostRuns} estimated reports</small></td><td class="mm">{row.limitEvents}</td>
        </tr>{/each}</tbody></table></div>{:else}<p class="empty">No execution evidence matches this view.</p>{/if}
    </section>
  {/if}
</div>

<style>
  .analytics { padding: 20px 24px 32px; height: 100%; overflow: auto; }
  .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .intro, .stamp { margin: 12px 0; }
  .muted, small { color: var(--text-2); font-size: 12px; line-height: 1.6; }
  .error { color: var(--danger-text); }
  .notice { background: var(--surface-2); padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); }
  .statistics { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 16px; border-block: 1px solid var(--border); padding: 16px 0; margin: 20px 0 8px; }
  dt { color: var(--text-2); font-size: 12px; }
  dd { margin: 6px 0 0; font-family: var(--mono); font-variant-numeric: tabular-nums; }
  .statistics dd { font-size: 20px; }
  section { margin-top: 28px; min-width: 0; }
  h2 { font-size: 14px; font-weight: 600; margin: 0 0 12px; }
  .legend { display: flex; align-items: center; gap: 8px; color: var(--text-2); font-size: 12px; }
  .created-key, .completed-key { width: 10px; height: 10px; }
  .created-key, .created-bar { background: var(--accent); }
  .completed-key, .completed-bar { background: var(--ok); }
  .chart-scroll, .table-scroll { overflow-x: auto; }
  .execution-table { min-width: 900px; }
  .chart { display: flex; gap: 8px; min-width: max-content; border-bottom: 1px solid var(--border); padding-top: 12px; }
  .day { width: 32px; flex: 1; font-size: 11.5px; color: var(--text-2); text-align: center; }
  .bars { height: 112px; display: flex; align-items: flex-end; gap: 2px; padding: 0 3px; }
  .bars span { flex: 1; }
  .day > span { display: block; padding: 6px 0; }
  details { margin-top: 10px; }
  summary { cursor: pointer; font-size: 12px; }
  .breakdowns { display: grid; grid-template-columns: 1fr 0.8fr 1.4fr; gap: 28px; }
  .facts { margin: 0; }
  .facts div { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; border-bottom: 1px solid var(--border); padding: 7px 0; }
  .facts dd { margin: 0; font-size: 12px; text-align: right; }
  .attempt-count { font-size: 13px; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; text-align: left; }
  th { color: var(--text-2); font-weight: 500; }
  td, th { padding: 10px 12px 10px 0; border-bottom: 1px solid var(--border); vertical-align: top; }
  td small { display: block; white-space: nowrap; }
  @media (max-width: 1100px) { .statistics { grid-template-columns: repeat(3, minmax(0, 1fr)); } .breakdowns { grid-template-columns: 1fr; gap: 0; } }
  @media (max-width: 899px) { .analytics { height: auto; overflow: visible; padding: 16px; } .statistics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .facts div { flex-wrap: wrap; } }
</style>
