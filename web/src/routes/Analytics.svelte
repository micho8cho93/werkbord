<script lang="ts">
  import Tour from '../lib/Tour.svelte';
  import { onMount, untrack } from 'svelte';
  import { api } from '../lib/api';
  import { analyticsDuration, executionTotals, measuredCost, measuredNumber, type AnalyticsReport } from '../lib/analytics';
  import { agentLabel } from '../lib/execution';
  import { app } from '../lib/state.svelte';
  import type { RunState } from '../lib/types';

  import AnalyticsBars from '../lib/AnalyticsBars.svelte';
  import AnalyticsDaily from '../lib/AnalyticsDaily.svelte';
  const views = ['Activity', 'Execution', 'Usage', 'Completion'] as const;
  type View = typeof views[number];
  let view = $state<View>('Activity');
  let filters = $state<Record<View, { project: string; days: number; provider: string; runner: string }>>({
    Activity: { project: '', days: 30, provider: '', runner: '' },
    Execution: { project: '', days: 30, provider: '', runner: '' },
    Usage: { project: '', days: 30, provider: '', runner: '' },
    Completion: { project: '', days: 30, provider: '', runner: '' },
  });
  const scope = $derived(filters[view]);
  let series = $state('both');
  let measure = $state('tokens');
  function tabKey(event: KeyboardEvent, index: number) {
    const offset = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (!offset && event.key !== 'Home' && event.key !== 'End') return;
    event.preventDefault();
    view = views[event.key === 'Home' ? 0 : event.key === 'End' ? views.length - 1 : (index + offset + views.length) % views.length];
    document.getElementById(`analytics-tab-${view}`)?.focus();
  }
  let report = $state<AnalyticsReport | null>(null);
  let loading = $state(true);
  let error = $state('');
  let requestId = 0;
  let chartScope = $state('');
  let mounted = true;
  onMount(() => {
    const timer = setInterval(() => { if (!document.hidden) void load(); }, 15000);
    return () => { mounted = false; clearInterval(timer); };
  });
  $effect(() => {
    void [scope.project, scope.days, scope.provider, app.analyticsRevision, view];
    loading = true;
    ++requestId;
    const timer = setTimeout(() => untrack(() => void load()), 120);
    return () => clearTimeout(timer);
  });

  async function load() {
    const id = ++requestId;
    const requestedScope = `${scope.project}/${scope.days}/${scope.provider}/${scope.runner}`;
    loading = true; error = '';
    try {
      const next = await api.analytics(scope.project, scope.days, scope.provider);
      if (mounted && id === requestId) { report = next; chartScope = requestedScope; }
    } catch (e) {
      if (mounted && id === requestId) error = e instanceof Error ? e.message : 'Could not load analytics.';
    } finally {
      if (mounted && id === requestId) loading = false;
    }
  }
  const projectName = (id: string) => app.project(id)?.name ?? id;
  const runnerName = (id: string) => app.runners.find((r) => r.id === id)?.name ?? (id || 'Not recorded');
  const outcomes: [RunState, string][] = [['completed', 'Normal exit'], ['failed', 'Failed'], ['stopped', 'Stopped'], ['blocked', 'Blocked'], ['waiting_for_user', 'Waiting'], ['starting', 'Starting'], ['running', 'Running']];
  const rows = $derived(report?.executions.filter(row => !scope.runner || row.runnerId === (scope.runner === '__unrecorded' ? '' : scope.runner)) ?? []);
  const totals = $derived(executionTotals(rows));
  const runners = $derived([...new Set([...(report?.executions.map(row => row.runnerId || '__unrecorded') ?? []), ...(scope.runner ? [scope.runner] : [])])]);
  const outcomeBars = $derived(outcomes.map(([state, label]) => ({ label, value: totals.outcomes[state] ?? 0, tone: state === 'completed' ? 'var(--ok)' : state === 'failed' ? 'var(--danger)' : state === 'waiting_for_user' ? 'var(--warn)' : 'var(--accent)' })));
  const usageBars = $derived(measure === 'tokens' ? [
    { label: 'Input tokens', value: totals.inputTokens, detail: `${totals.inputRuns}/${totals.attempts} runs reported input` },
    { label: 'Output tokens', value: totals.outputTokens, detail: `${totals.outputRuns}/${totals.attempts} runs reported output` },
    { label: 'Cached tokens', value: totals.cachedTokens, detail: `${totals.cachedRuns}/${totals.attempts} runs reported cache; may overlap input tokens`, tone: 'var(--text-2)' },
  ] : [
    { label: 'Reported API cost', value: totals.actualCostUsd, formatted: measuredCost(totals.actualCostUsd), detail: `${totals.actualCostRuns}/${totals.attempts} runs reported cost` },
    { label: 'Estimated API equivalent', value: totals.estimatedCostUsd, formatted: measuredCost(totals.estimatedCostUsd), detail: `${totals.estimatedCostRuns}/${totals.attempts} runs reported estimates`, tone: 'var(--text-2)' },
  ]);
  const hasThroughput = $derived(!!report?.days.some((day) => day.created || day.completed));
  let availableProviders = $state<string[]>([]);
  $effect(() => { if (report && !report.provider) availableProviders = report.providers; });
</script>

<div class="analytics">
  <Tour surface="analytics" />
  <div class="tabs" role="tablist" aria-label="Analytics views">
    {#each views as tab, i (tab)}<button id="analytics-tab-{tab}" role="tab" aria-selected={view === tab} aria-controls="analytics-panel" tabindex={view === tab ? 0 : -1} onclick={() => view = tab} onkeydown={event => tabKey(event, i)}>{tab}</button>{/each}
  </div>
  <div id="analytics-panel" role="tabpanel" aria-labelledby="analytics-tab-{view}" tabindex="0">
  <div class="filters" aria-busy={loading}>
    <label class="pick"><span>Project</span><select bind:value={scope.project} aria-label="Analytics project"><option value="">All projects</option>{#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}</select></label>
    <label class="pick"><span>Time range</span><select bind:value={scope.days} aria-label="Analytics time range"><option value={7}>Last 7 days</option><option value={30}>Last 30 days</option><option value={90}>Last 90 days</option></select></label>
    {#if view !== 'Activity'}<label class="pick"><span>Provider</span><select bind:value={scope.provider} aria-label="Analytics agent provider"><option value="">All agent providers</option>{#each [...new Set([...availableProviders, ...(scope.provider ? [scope.provider] : [])])] as p (p)}<option value={p}>{agentLabel(app.agents, p)}</option>{/each}</select></label>{/if}
    {#if view === 'Execution' || view === 'Usage'}<label class="pick"><span>Runner</span><select bind:value={scope.runner} aria-label="Analytics runner"><option value="">All runners</option>{#each runners as id (id)}<option value={id}>{runnerName(id === '__unrecorded' ? '' : id)}</option>{/each}</select></label>{/if}
    {#if view === 'Activity'}<label class="pick"><span>Show</span><select bind:value={series} aria-label="Activity chart series"><option value="both">Created and Done</option><option value="created">Created</option><option value="completed">Moved to Done</option></select></label>{/if}
    {#if view === 'Usage'}<label class="pick"><span>Measure</span><select bind:value={measure} aria-label="Usage measure"><option value="tokens">Tokens</option><option value="cost">API cost</option></select></label>{/if}
    {#if loading}<span class="muted" role="status">Updating…</span>{/if}
  </div>
  {#if error}<p role="alert" class="error">{error} <button class="btn small" onclick={() => void load()}>Try again</button></p>
  {:else if loading && !report}<p class="empty" role="status">Loading analytics…</p>
  {:else if report}
    <p class="muted stamp">As of {new Date(report.end).toLocaleString()} · {report.projectId ? projectName(report.projectId) : 'All projects'} · {Math.round((Date.parse(report.end) - Date.parse(report.start)) / 86400000)} days{loading ? ' · showing the previous snapshot while updating' : ''}</p>
    {#if report.truncated || report.retentionRemoved || report.missingTaskHistory}
      <p class="notice" role="status">Partial history. {report.truncated ? 'The bounded read omitted older records. ' : ''}{report.retentionRemoved ? 'Event retention has removed some evidence. ' : ''}{report.missingTaskHistory ? `${report.missingTaskHistory} Done records lack a known preceding state and were excluded. ` : ''}Counts may be lower than the full history.</p>
    {/if}
    {#if view === 'Activity'}
      <dl class="statistics">
        <div><dt>Tasks created</dt><dd>{report.created}</dd></div><div><dt>Moved to Done</dt><dd>{report.completed}</dd></div>
        <div><dt>Median task duration</dt><dd>{analyticsDuration(report.medianTaskSeconds)}</dd></div><div><dt>Duration samples</dt><dd>{report.durationSamples}</dd></div>
      </dl>
      <p class="muted">Duration includes waiting, from creation to the first recorded move to Done. Average: {analyticsDuration(report.averageTaskSeconds)}.</p>
      <section aria-labelledby="throughput-title"><h2 id="throughput-title">Created and completed over time</h2>
        {#if hasThroughput}<AnalyticsDaily days={report.days} {series} scope={chartScope} />
        {:else}<p class="empty">No task activity in this period. Try a longer time range or another project.</p>{/if}
      </section>
      <details class="disclosure"><summary>Work categories</summary><p class="muted">Current labels; tasks with multiple labels appear in each category.</p><AnalyticsBars scope={`${chartScope}/${measure}/${scope.runner}`} label="Task activity by category" rows={report.categories.map(c => ({ label: c.name, value: c.completed, detail: `${c.created} created · ${c.completed} moved to Done`, tone: 'var(--ok)' }))} />{#if !report.categories.length}<p class="muted">No task activity matches these categories.</p>{/if}</details>
    {:else if view === 'Execution'}
      <dl class="statistics"><div><dt>Run attempts</dt><dd>{totals.attempts}</dd></div><div><dt>Retries</dt><dd>{totals.retries}</dd></div><div><dt>Failed runs</dt><dd>{totals.outcomes.failed ?? 0}</dd></div><div><dt>Waiting for you</dt><dd>{totals.outcomes.waiting_for_user ?? 0}</dd></div></dl>
      <p class="muted">Latest outcomes of runs started in the selected period. A normal exit does not verify task completion.</p>
      <section><h2>Run outcomes</h2>{#if totals.attempts}<AnalyticsBars scope={`${chartScope}/${measure}/${scope.runner}`} rows={outcomeBars} label="Run outcomes by count" />{:else}<p class="empty">No runs match these filters. Try another provider, runner or time range.</p>{/if}</section>
      <details class="disclosure"><summary>Current task state</summary><p class="muted">Across the selected project and provider, regardless of runner or run start date.</p><dl class="facts"><div><dt>Currently blocked</dt><dd>{report.blocked}</dd></div><div><dt>Currently stopped</dt><dd>{report.stopped}</dd></div></dl></details>
    {:else if view === 'Usage'}
      <dl class="statistics"><div><dt>Input tokens</dt><dd>{measuredNumber(totals.inputTokens)}</dd></div><div><dt>Output tokens</dt><dd>{measuredNumber(totals.outputTokens)}</dd></div><div><dt>Reported API cost</dt><dd>{measuredCost(totals.actualCostUsd)}</dd></div><div><dt>Usage-limit notices</dt><dd>{totals.limitEvents}</dd></div></dl>
      <p class="muted">Measured usage from runs started in this period. Missing reports remain unknown; subscription charges and remaining quota are unknown.</p>
      <section><h2>{measure === 'tokens' ? 'Token usage and reporting coverage' : 'Reported and estimated API cost'}</h2>{#if totals.attempts}<AnalyticsBars scope={`${chartScope}/${measure}/${scope.runner}`} rows={usageBars} label={measure === 'tokens' ? 'Token usage' : 'API cost in USD'} />{:else}<p class="empty">No execution evidence matches these filters. Try another provider, runner or time range.</p>{/if}</section>
      <details class="disclosure"><summary>Reporting details</summary><p class="muted">{totals.partialRuns} partial reports across {totals.attempts} runs. Cached tokens may overlap input tokens. API cost and estimated equivalents are separate measures and are not added together.</p></details>
    {:else}
      <dl class="statistics"><div><dt>Accepted</dt><dd>{report.accepted}</dd></div><div><dt>Rejected</dt><dd>{report.rejected}</dd></div><div><dt>Controller-verified merges</dt><dd>{report.verifiedMerges}</dd></div><div><dt>Normal session ends</dt><dd>{report.normalSessionCompletions}</dd></div></dl>
      <p class="muted">Assessments, board decisions, session ends and verified merges are separate signals, each with its own count.</p>
      <div class="breakdowns"><section><h2>Recorded assessments</h2><AnalyticsBars scope={`${chartScope}/${measure}/${scope.runner}`} label="Accepted and rejected work" rows={[{ label: 'Accepted', value: report.accepted, tone: 'var(--ok)' }, { label: 'Rejected', value: report.rejected, tone: 'var(--danger)' }]} />{#if !report.accepted && !report.rejected}<p class="muted">No assessments recorded for runs in this period.</p>{/if}</section>
      <section><h2>Completion signals</h2><AnalyticsBars scope={`${chartScope}/${measure}/${scope.runner}`} label="Separate completion signals" rows={[{ label: 'Moved to Done', value: report.completed, tone: 'var(--ok)' }, { label: 'Controller-verified merges', value: report.verifiedMerges, tone: 'var(--text-2)' }, { label: 'Normal session ends', value: report.normalSessionCompletions }]} /></section></div>
      <details class="disclosure"><summary>Completion evidence</summary>
        <dl class="facts">
          <div><dt>Agent-reported task completion</dt><dd>{report.agentReportedCompletion ?? 'Not recorded'}</dd></div>
          <div><dt>Normal session ends</dt><dd>{report.normalSessionCompletions}</dd></div>
          <div><dt>Independently verified task completion</dt><dd>{report.verifiedCompletion ?? 'Not recorded'}</dd></div>
          <div><dt>Accepted / rejected</dt><dd>{report.accepted} / {report.rejected}</dd></div>
          <div><dt>Controller-verified merges</dt><dd>{report.verifiedMerges}</dd></div>
        </dl>
        <p class="muted">Session ends, agent reports, board decisions and merge verification are separate signals.</p>
      </details>
    {/if}
    {#if view === 'Execution' || view === 'Usage'}
    <details class="disclosure breakdown" aria-labelledby="breakdown-title"><summary id="breakdown-title">Breakdown</summary>
      {#if rows.length}<!-- svelte-ignore a11y_no_noninteractive_tabindex (The breakdown needs a keyboard-focusable horizontal scroll region.) -->
        <div class="table-scroll" role="region" aria-label="Runner, provider and model execution breakdown" tabindex="0"><table class="execution-table"><thead><tr>{#each ['Project', 'Runner', 'Agent provider', 'Configured model', 'Attempts / retries', 'Input / output tokens', 'API cost / estimated', 'Limit notices'] as label (label)}<th>{label}</th>{/each}</tr></thead>
        <tbody>{#each rows as row (`${row.projectId}/${row.runnerId}/${row.provider}/${row.model}`)}<tr>
          <td>{projectName(row.projectId)}</td><td>{runnerName(row.runnerId)}</td><td>{agentLabel(app.agents, row.provider)}</td><td>{row.model || 'Agent default · unknown'}</td>
          <td class="mm">{row.attempts} / {row.retries}</td><td class="mm">{measuredNumber(row.inputTokens)} / {measuredNumber(row.outputTokens)}<small>{row.inputRuns} input, {row.outputRuns} output reports</small></td>
          <td class="mm">{measuredCost(row.actualCostUsd)} / {measuredCost(row.estimatedCostUsd)}<small>{row.actualCostRuns} actual, {row.estimatedCostRuns} estimated reports</small></td><td class="mm">{row.limitEvents}</td>
        </tr>{/each}</tbody></table></div>{:else}<p class="empty">No execution evidence matches this view.</p>{/if}
    </details>
    {/if}
  {/if}
  </div>
</div>

<style>
  .analytics { display: flex; flex-direction: column; gap: 16px; padding: 20px 24px 32px; height: 100%; overflow: auto; }
  /* The views are a segmented control; the filters and the figures each sit in a panel of their own. */
  .tabs { display: inline-flex; align-self: flex-start; max-width: 100%; padding: 2px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-2); box-shadow: var(--tray-sh); overflow-x: auto; }
  .tabs button { flex: none; height: 30px; padding: 0 14px; border: 0; border-radius: 4px; background: transparent; color: var(--text-2); font: inherit; font-size: 13px; font-weight: 500; cursor: pointer; }
  .tabs button:hover { color: var(--text); }
  .tabs button[aria-selected="true"] { background: var(--btn-bg); box-shadow: var(--btn-sh); color: var(--text); }
  #analytics-panel { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
  .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-tray); background: var(--surface); box-shadow: var(--card-sh); }
  .stamp { margin: 0; }
  .muted, small { color: var(--text-2); font-size: 12px; line-height: 1.6; }
  p { margin: 0; }
  .error { color: var(--danger-text); }
  .notice { background: var(--surface-2); padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); }
  .statistics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin: 0; }
  .statistics > div { padding: 14px 16px; border: 1px solid var(--border); border-radius: var(--radius-tray); background: var(--surface); box-shadow: var(--card-sh); min-width: 0; }
  dt { color: var(--text-2); font-size: 12px; }
  dd { margin: 6px 0 0; font-family: var(--mono); font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
  .statistics dd { font-size: 22px; }
  /* A section is a panel: a title, then what it shows, inside a border of its own. */
  section { min-width: 0; padding: 16px; border: 1px solid var(--border); border-radius: var(--radius-tray); background: var(--surface); box-shadow: var(--card-sh); }
  h2 { font-size: 14px; font-weight: 600; margin: 0 0 16px; }
  .table-scroll { overflow-x: auto; }
  .execution-table { min-width: 900px; }
  details { margin: 0; }
  summary { cursor: pointer; font-size: 13px; min-height: 24px; }
  .breakdowns { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .disclosure { min-width: 0; padding: 12px 16px; border: 1px solid var(--border); border-radius: var(--radius-tray); background: var(--surface); box-shadow: var(--card-sh); }
  .disclosure[open] > summary { margin-bottom: 14px; padding-bottom: 12px; border-bottom: 1px solid var(--border); }
  .facts { margin: 0; }
  .facts div { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; border-bottom: 1px solid var(--border); padding: 7px 0; }
  .facts div:last-child { border-bottom: 0; }
  .facts dd { margin: 0; font-size: 12px; text-align: right; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; text-align: left; }
  th { color: var(--text-2); font-weight: 500; }
  td, th { padding: 10px 12px 10px 0; border-bottom: 1px solid var(--border); vertical-align: top; }
  td small { display: block; white-space: nowrap; }
  @media (max-width: 1100px) { .statistics { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  @media (max-width: 899px) { .analytics { height: auto; overflow: visible; padding: 16px 16px 24px; } .breakdowns { grid-template-columns: 1fr; } .facts div { flex-wrap: wrap; } .filters .pick { flex: 1 1 140px; min-width: 0; } .filters select { min-width: 0; max-width: 100%; } summary { min-height: 44px; } }
</style>
