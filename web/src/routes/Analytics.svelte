<script lang="ts">
  import Tour from '../lib/Tour.svelte';
  import { onMount, untrack } from 'svelte';
  import { api } from '../lib/api';
  import { analyticsDuration, executionTotals, measuredCost, measuredNumber, percent, sumBy, type AnalyticsReport } from '../lib/analytics';
  import { agentLabel } from '../lib/execution';
  import { app } from '../lib/state.svelte';
  import type { RunState } from '../lib/types';

  import AnalyticsBars from '../lib/AnalyticsBars.svelte';
  import AnalyticsDaily from '../lib/AnalyticsDaily.svelte';
  import Donut from '../lib/charts/Donut.svelte';
  import Heat from '../lib/charts/Heat.svelte';
  import LineProgress from '../lib/charts/LineProgress.svelte';
  import Panel from '../lib/charts/Panel.svelte';
  import Ring from '../lib/charts/Ring.svelte';
  import Stack from '../lib/charts/Stack.svelte';
  import StatTile from '../lib/charts/StatTile.svelte';
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
  const createdLine = $derived(report?.days.map((d) => d.created) ?? []);
  const doneLine = $derived(report?.days.map((d) => d.completed) ?? []);
  const heatDays = $derived(report?.days.map((d) => ({ date: d.date, value: d.created + d.completed })) ?? []);
  const doneShare = $derived(report && report.created > 0 ? report.completed / report.created : null);
  const outcomeSlices = $derived(outcomeBars.map((b) => ({ label: b.label, value: b.value, tone: b.tone })));
  const providerBars = $derived(sumBy(rows, 'provider', (r) => r.attempts).map((r) => ({ label: agentLabel(app.agents, r.key), value: r.value, tone: 'var(--accent)' })));
  const runnerBars = $derived(sumBy(rows, 'runnerId', (r) => r.attempts).map((r) => ({ label: runnerName(r.key), value: r.value, tone: 'var(--block)' })));
  const tokenProvider = $derived(measure === 'tokens'
    ? sumBy(rows, 'provider', (r) => (r.inputTokens == null && r.outputTokens == null ? null : (r.inputTokens ?? 0) + (r.outputTokens ?? 0))).map((r) => ({ label: agentLabel(app.agents, r.key), value: r.value, tone: 'var(--warn)' }))
    : sumBy(rows, 'provider', (r) => r.actualCostUsd).map((r) => ({ label: agentLabel(app.agents, r.key), value: r.value, formatted: measuredCost(r.value), tone: 'var(--warn)' })));
  const coverage = $derived([
    { label: 'Input tokens', ran: totals.inputRuns }, { label: 'Output tokens', ran: totals.outputRuns }, { label: 'Cached tokens', ran: totals.cachedRuns },
    { label: 'Reported API cost', ran: totals.actualCostRuns }, { label: 'Estimated API equivalent', ran: totals.estimatedCostRuns },
  ].map((c) => ({ label: c.label, value: c.ran, formatted: `${c.ran} of ${totals.attempts} runs`, detail: percent(c.ran, totals.attempts) === null ? undefined : `${percent(c.ran, totals.attempts)}% of runs reported it`, tone: 'var(--block)' })));
  const signals = $derived(report ? [
    { label: 'Run attempts', value: totals.attempts, tone: 'var(--accent)' },
    { label: 'Normal session ends', value: report.normalSessionCompletions, tone: 'var(--accent)' },
    { label: 'Moved to Done', value: report.completed, tone: 'var(--ok)' },
    { label: 'Accepted by you', value: report.accepted, tone: 'var(--ok)' },
    { label: 'Controller-verified merges', value: report.verifiedMerges, tone: 'var(--block)' },
  ] : []);
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
  {#if error}<p role="alert" class="error">{error} <button class="btn small quiet" onclick={() => void load()}>Try again</button></p>
  {:else if loading && !report}<p class="empty" role="status">Loading analytics…</p>
  {:else if report}
    <p class="muted stamp">As of {new Date(report.end).toLocaleString()} · {report.projectId ? projectName(report.projectId) : 'All projects'} · {Math.round((Date.parse(report.end) - Date.parse(report.start)) / 86400000)} days{loading ? ' · showing the previous snapshot while updating' : ''}</p>
    {#if report.truncated || report.retentionRemoved || report.missingTaskHistory}
      <p class="notice" role="status">Partial history. {report.truncated ? 'The bounded read omitted older records. ' : ''}{report.retentionRemoved ? 'Event retention has removed some evidence. ' : ''}{report.missingTaskHistory ? `${report.missingTaskHistory} Done records lack a known preceding state and were excluded. ` : ''}Counts may be lower than the full history.</p>
    {/if}
    {#if view === 'Activity'}
      <dl class="statistics">
        <StatTile label="Tasks created" value={report.created} tone="var(--accent)" spark={createdLine} sparkLabel="Tasks created per day" />
        <StatTile label="Moved to Done" value={report.completed} tone="var(--ok)" spark={doneLine} sparkLabel="Tasks moved to Done per day" />
        <StatTile label="Median task duration" value={analyticsDuration(report.medianTaskSeconds)} hint="Average {analyticsDuration(report.averageTaskSeconds)}" tone="var(--block)" />
        <StatTile label="Duration samples" value={report.durationSamples} hint="Tasks the duration is drawn from" tone="var(--warn)" />
      </dl>
      <p class="muted">Duration includes waiting, from creation to the first recorded move to Done. Average: {analyticsDuration(report.averageTaskSeconds)}.</p>
      {#if hasThroughput}
      <div class="board">
        <Panel id="throughput-title" title="Created and completed over time" caption="Tasks per day, by UTC day" tone="var(--accent)" span={7}><AnalyticsDaily days={report.days} {series} scope={chartScope} /></Panel>
        <Panel title="Progress so far" caption="Running totals within this period; the band shows their difference" tone="var(--ok)" span={5}><LineProgress days={report.days} {series} /></Panel>
        <Panel title="Done against created" caption="Tasks moved to Done for each one created" tone="var(--ok)" span={3}>
          <Ring value={doneShare} tone="var(--ok)" caption={doneShare === null ? 'nothing created' : 'Done / created'} label="Moves to Done divided by tasks created in this period" />
          <p class="muted tail">{report.completed} done · {report.created} created{report.completed > report.created ? '. More were finished than started: some were created before this period.' : '.'}</p>
        </Panel>
        <Panel title="Busiest days" caption="Created plus moved to Done, a square for each day" tone="var(--block)" span={5}><Heat days={heatDays} tone="var(--block)" label="Activity for each day of the period" /></Panel>
        <Panel title="Work categories" caption="Current labels; a task with several appears in each" tone="var(--warn)" span={4}>
          <AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} label="Task activity by category" rows={report.categories.map(c => ({ label: c.name, value: c.completed, detail: `${c.created} created · ${c.completed} moved to Done`, tone: 'var(--warn)' }))} />{#if !report.categories.length}<p class="muted">No task activity matches these categories.</p>{/if}
        </Panel>
      </div>
      {:else}<p class="empty">No task activity in this period. Try a longer time range or another project.</p>{/if}
    {:else if view === 'Execution'}
      <dl class="statistics">
        <StatTile label="Run attempts" value={totals.attempts} tone="var(--accent)" />
        <StatTile label="Retries" value={totals.retries} hint={percent(totals.retries, totals.attempts) === null ? '' : `${percent(totals.retries, totals.attempts)}% of attempts`} tone="var(--warn)" />
        <StatTile label="Failed runs" value={totals.outcomes.failed ?? 0} tone="var(--danger)" />
        <StatTile label="Waiting for you" value={totals.outcomes.waiting_for_user ?? 0} tone="var(--amber)" />
      </dl>
      <p class="muted">Latest outcomes of runs started in the selected period. A normal exit does not verify task completion.</p>
      <div class="board">
        <Panel title="Run outcomes" caption="Where each attempt ended up" tone="var(--accent)" span={5}>{#if totals.attempts}<Donut slices={outcomeSlices} centre={String(totals.attempts)} caption={totals.attempts === 1 ? 'run' : 'runs'} label="Run outcomes by count" />{:else}<p class="empty">No runs match these filters. Try another provider, runner or time range.</p>{/if}</Panel>
        <Panel title="Reliability" caption="Share of attempts" tone="var(--ok)" span={4}>
          <div class="rings"><div><Ring value={percent(totals.outcomes.completed ?? 0, totals.attempts) === null ? null : (totals.outcomes.completed ?? 0) / totals.attempts} tone="var(--ok)" caption="normal exit" label="Share of attempts that ended normally" /></div><div><Ring value={totals.attempts ? totals.retries / totals.attempts : null} tone="var(--warn)" caption="retried" label="Share of attempts that were retries" /></div></div>
        </Panel>
        <Panel title="Task state now" caption="Across the project and provider, any runner or start date" tone="var(--danger)" span={3}>
          <dl class="facts"><div><dt>Currently blocked</dt><dd>{report.blocked}</dd></div><div><dt>Currently stopped</dt><dd>{report.stopped}</dd></div></dl>
        </Panel>
        <Panel title="Attempts by agent provider" caption="Which runtime did the work" tone="var(--accent)" span={6}><AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} rows={providerBars} label="Attempts by agent provider" />{#if !providerBars.length}<p class="muted">No attempts to group.</p>{/if}</Panel>
        <Panel title="Attempts by runner" caption="Which machine ran it" tone="var(--block)" span={6}><AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} rows={runnerBars} label="Attempts by runner" />{#if !runnerBars.length}<p class="muted">No attempts to group.</p>{/if}</Panel>
      </div>
    {:else if view === 'Usage'}
      <dl class="statistics">
        <StatTile label="Input tokens" value={measuredNumber(totals.inputTokens)} tone="var(--accent)" />
        <StatTile label="Output tokens" value={measuredNumber(totals.outputTokens)} tone="var(--ok)" />
        <StatTile label="Reported API cost" value={measuredCost(totals.actualCostUsd)} tone="var(--warn)" />
        <StatTile label="Usage-limit notices" value={totals.limitEvents} tone="var(--danger)" />
      </dl>
      <p class="muted">Measured usage from runs started in this period. Missing reports remain unknown; subscription charges and remaining quota are unknown.</p>
      <div class="board">
        <Panel title={measure === 'tokens' ? 'Token usage and reporting coverage' : 'Reported and estimated API cost'} caption={measure === 'tokens' ? 'Input against output; cached tokens are counted apart' : 'Two separate measures, never added together'} tone="var(--accent)" span={5}>
          {#if !totals.attempts}<p class="empty">No execution evidence matches these filters. Try another provider, runner or time range.</p>
          {:else if measure === 'tokens'}
            {#if totals.inputTokens !== null && totals.outputTokens !== null && totals.inputTokens + totals.outputTokens > 0}<Donut slices={[{ label: 'Input tokens', value: totals.inputTokens ?? 0, tone: 'var(--accent)' }, { label: 'Output tokens', value: totals.outputTokens ?? 0, tone: 'var(--ok)' }]} centre={measuredNumber((totals.inputTokens ?? 0) + (totals.outputTokens ?? 0))} caption="tokens" label="Input and output tokens" />{/if}
            <AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} rows={usageBars} label="Token usage" />
          {:else}<AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} rows={usageBars} label="API cost in USD" />{/if}
        </Panel>
        <Panel title="Reporting coverage" caption="How many runs reported each figure" tone="var(--block)" span={7}>{#if totals.attempts}<AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} ceiling={totals.attempts} rows={coverage} label="Runs that reported each measure" />{:else}<p class="empty">No runs to report on.</p>{/if}</Panel>
        <Panel title={measure === 'tokens' ? 'Tokens by agent provider' : 'Reported cost by agent provider'} caption="Input plus output, or reported cost" tone="var(--warn)" span={6}><AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} rows={tokenProvider} label="Usage by agent provider" />{#if !tokenProvider.length}<p class="muted">Nothing to group.</p>{/if}</Panel>
        <Panel title="Attempts by runner" caption="Which machine used it" tone="var(--block)" span={6}><AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} rows={runnerBars} label="Attempts by runner" />{#if !runnerBars.length}<p class="muted">Nothing to group.</p>{/if}</Panel>
      </div>
      <details class="disclosure"><summary>Reporting details</summary><p class="muted">{totals.partialRuns} partial reports across {totals.attempts} runs. Cached tokens may overlap input tokens. API cost and estimated equivalents are separate measures and are not added together.</p></details>
    {:else}
      <dl class="statistics">
        <StatTile label="Accepted" value={report.accepted} tone="var(--ok)" />
        <StatTile label="Rejected" value={report.rejected} tone="var(--danger)" />
        <StatTile label="Controller-verified merges" value={report.verifiedMerges} tone="var(--block)" />
        <StatTile label="Normal session ends" value={report.normalSessionCompletions} tone="var(--accent)" />
      </dl>
      <p class="muted">Assessments, board decisions, session ends and verified merges are separate signals, each with its own count.</p>
      <div class="board">
        <Panel title="Recorded assessments" caption="Your decision on each assessed run" tone="var(--ok)" span={5}>
          {#if report.accepted + report.rejected > 0}
            <div class="assess"><Ring value={report.accepted / (report.accepted + report.rejected)} tone="var(--ok)" caption="accepted" label="Share of assessed runs you accepted" /><Stack label="Accepted and rejected work" parts={[{ label: 'Accepted', value: report.accepted, tone: 'var(--ok)' }, { label: 'Rejected', value: report.rejected, tone: 'var(--danger)' }]} /></div>
          {:else}<p class="muted">No assessments recorded for runs in this period.</p>{/if}
        </Panel>
        <Panel title="Completion signals" caption="Each is its own count, not steps of one" tone="var(--accent)" span={7}><AnalyticsBars shares={false} scope={`${chartScope}/${measure}/${scope.runner}`} label="Separate completion signals" rows={[{ label: 'Moved to Done', value: report.completed, tone: 'var(--ok)' }, { label: 'Controller-verified merges', value: report.verifiedMerges, tone: 'var(--block)' }, { label: 'Normal session ends', value: report.normalSessionCompletions, tone: 'var(--accent)' }]} /></Panel>
        <Panel title="Signals against attempts" caption="How far each signal reaches, compared on one count scale" tone="var(--block)" span={12}>
          {#if totals.attempts}<div class="funnel">{#each signals as s (s.label)}<div class="step" style:--tone={s.tone}><span class="nm">{s.label}</span><span class="meter"><span class="fill" style:width="{Math.min(100, (s.value / Math.max(1, ...signals.map(signal => signal.value))) * 100)}%"></span></span><strong class="mm">{s.value}</strong></div>{/each}</div>
          {:else}<p class="empty">No runs match these filters.</p>{/if}
        </Panel>
      </div>
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
  .analytics { display: flex; flex-direction: column; gap: 16px; padding: 20px clamp(16px, 2vw, 32px) 32px; height: 100%; overflow: auto; }
  .analytics > :global(*) { flex: none; }
  /* The views are a segmented control; the filters, the figures and each chart sit in panels of their own. */
  .tabs { display: inline-flex; align-self: flex-start; max-width: 100%; padding: 2px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-2); box-shadow: var(--tray-sh); overflow-x: auto; }
  .tabs button { flex: none; height: 30px; padding: 0 14px; border: 0; border-radius: 4px; background: transparent; color: var(--text-2); font: inherit; font-size: 13px; font-weight: 500; cursor: pointer; }
  .tabs button:hover { color: var(--text); }
  .tabs button[aria-selected="true"] { background: var(--btn-bg); box-shadow: var(--btn-sh), 0 0 0 1px var(--border); color: var(--text); }
  #analytics-panel { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
  .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-tray); background: var(--surface); box-shadow: var(--card-sh); }
  .stamp { margin: 0; }
  .muted, small { color: var(--text-2); font-size: 12px; line-height: 1.6; }
  .tail { margin-top: 10px; text-align: center; }
  p { margin: 0; }
  .error { color: var(--danger-text); }
  .notice { background: var(--surface-2); padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); }
  .statistics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; margin: 0; }
  /* The charts: a twelve-column board, each panel taking the width its picture needs. */
  .board { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 14px; align-items: stretch; }
  .rings { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .assess { display: grid; gap: 18px; justify-items: stretch; }
  .funnel { display: grid; gap: 12px; }
  .step { display: grid; grid-template-columns: minmax(150px, 22%) minmax(0, 1fr) 56px; align-items: center; gap: 14px; font-size: 13px; }
  .meter { height: 12px; border-radius: 6px; background: var(--surface-2); box-shadow: var(--tray-sh); overflow: hidden; }
  .fill { display: block; height: 100%; border-radius: 6px; background: linear-gradient(90deg, color-mix(in srgb, var(--tone) 55%, var(--surface)), var(--tone)); }
  .step strong { text-align: right; font-weight: 600; }
  dt { color: var(--text-2); font-size: 12px; }
  dd { margin: 6px 0 0; font-family: var(--mono); font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
  .table-scroll { overflow-x: auto; }
  .execution-table { min-width: 900px; }
  details { margin: 0; }
  summary { cursor: pointer; font-size: 13px; min-height: 24px; }
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
  @media (max-width: 1100px) { .statistics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .board { grid-template-columns: 1fr; } .step { grid-template-columns: 1fr 56px; } .step .meter { grid-column: 1 / -1; grid-row: 2; } }
  @media (max-width: 899px) { .analytics { height: auto; overflow: visible; padding: 16px 16px 24px; } .facts div { flex-wrap: wrap; } .filters .pick { flex: 1 1 140px; min-width: 0; } .filters select { min-width: 0; max-width: 100%; } summary { min-height: 44px; } }
</style>
