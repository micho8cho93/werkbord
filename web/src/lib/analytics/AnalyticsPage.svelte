<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';
  import AnalyticsUsageDetails from '../AnalyticsUsageDetails.svelte';
  import AnalyticsBars from '../AnalyticsBars.svelte';
  import AnalyticsDaily from '../AnalyticsDaily.svelte';
  import Donut from '../charts/Donut.svelte';
  import Heat from '../charts/Heat.svelte';
  import LineProgress from '../charts/LineProgress.svelte';
  import Panel from '../charts/Panel.svelte';
  import Ring from '../charts/Ring.svelte';
  import StatTile from '../charts/StatTile.svelte';
  import { analyticsDuration, measuredCost, measuredNumber, percent, sumBy } from '../analytics';
  import { activityOf, mergeActivity, RANGES, runsAvailable, runsOf } from '../../shell/analytics';
  import FilterBar from '../../shell/FilterBar.svelte';
  import Notice from '../../shell/Notice.svelte';
  import PageFrame from '../../shell/PageFrame.svelte';
  import Segmented from '../Segmented.svelte';
  import { agentName } from '../agent-marks';
  import type { Item } from '../../shell/types';
  import type { ProjectIdentity } from '../../shell/project-context';

  // One Analytics page, drawn once for the Mac window page and for the web app. Where to look (Personal and every team, Personal alone, or
  // one team, and a project within that) is the project picker's choice, handed in as `selected`; the person chooses a period; what each
  // workspace reports about the work itself is merged into one picture. What an agent did (runs, tokens, cost) is reported by Personal
  // alone and has a view of its own. Each side says which workspaces there are and how to read one's report (`read`).
  let { items, selected, label, read, names = { projects: {}, runners: {} }, revision = 0 }: {
    items: readonly Item[] | undefined;
    selected: ProjectIdentity | null;
    /** What the page says it is looking at: "All Projects", or the project and where it is. */
    label: string;
    read: (workspace: string, project: string, days: number) => Promise<unknown>;
    /** Moves when something happened that the figures may reflect (an agent's run ended): the page then reads again, quietly. */
    revision?: number;
    names?: { projects: Record<string, string>; runners: Record<string, string> };
  } = $props();

  const views = ['Work', 'Runs and usage', 'Outcomes'] as const;
  type View = (typeof views)[number];
  let view = $state<View>('Work');
  const where = $derived(selected?.workspace ?? 'all');
  const projectKey = $derived(selected?.project ?? '');
  let days = $state<number>(30);
  let series = $state('both');
  let provider = $state('');
  let runner = $state('');
  let measure = $state('tokens');
  $effect(() => { void projectKey; void where; untrack(() => { provider = ''; runner = ''; }); });
  const targets = $derived((items ?? []).filter(i => i.state === 'ready' && (!selected || i.id === selected.workspace))
    .map(i => ({ workspace: i.id, project: selected?.project ?? '' })));
  const nameOf = (id: string): string => items?.find(i => i.id === id)?.name ?? id;

  let parts = $state<{ workspace: string; raw: unknown }[]>([]);
  let failed = $state<{ workspace: string; message: string }[]>([]);
  let loading = $state(true);
  let loaded = $state(false);
  let asOf = $state('');
  let requestId = 0;
  // Where the page reads from, as one string. The workspace list is rebuilt every time it is refreshed, so depending on `targets`
  // itself would reload (and blank) the page every few seconds even though nothing about where it looks has changed.
  const targetKey = $derived(targets.map(t => `${t.workspace}/${t.project}`).join('|'));
  // A report's own timestamps move on every read; what the person sees only changes when something else does.
  const content = (list: { workspace: string; raw: unknown }[]) => JSON.stringify(list.map(p => ({ ...obj(p.raw), start: '', end: '', at: '', generatedAt: '', workspace: p.workspace })));
  const obj = (v: unknown): Record<string, unknown> => (v && typeof v === 'object' ? (v as Record<string, unknown>) : {});

  // A background refresh (silent) never shows "Updating…", never blanks the page and replaces the figures only when they changed, so the
  // charts stay put and do not replay their entrance while the page is read. A change the person makes (place, project, period) loads openly.
  async function load(silent = false): Promise<void> {
    const id = ++requestId;
    const wanted = untrack(() => ({ targets, days }));
    if (!silent) loading = true;
    const results = await Promise.all(
      wanted.targets.map(async (t) => {
        try {
          return { workspace: t.workspace, raw: await read(t.workspace, t.project, wanted.days) };
        } catch (e) {
          return { workspace: t.workspace, message: e instanceof Error ? e.message : String(e) };
        }
      }),
    );
    if (id !== requestId) return;
    const next = results.filter((r): r is { workspace: string; raw: unknown } => 'raw' in r);
    const problems = results.filter((r): r is { workspace: string; message: string } => 'message' in r);
    // A silent read that failed keeps what is already on show; the next one tries again.
    if (!silent || next.length || !parts.length) {
      if (!silent || content(next) !== content(parts)) parts = next;
      failed = problems;
      asOf = activityOf(next[0]?.raw ?? null).end;
    }
    loading = false;
    loaded = true;
  }

  $effect(() => {
    void [where, projectKey, days, targetKey];
    untrack(() => { parts = []; failed = []; loaded = false; loading = true; });
    ++requestId;
    const timer = setTimeout(() => void load(), 120);
    return () => clearTimeout(timer);
  });
  onMount(() => {
    const timer = setInterval(() => { if (!document.hidden && loaded) void load(true); }, 30000);
    return () => clearInterval(timer);
  });
  // Events can arrive every few seconds while agents run: at most one quiet read per interval, never one per event.
  let seenRevision = untrack(() => revision);
  let soon: ReturnType<typeof setTimeout> | undefined;
  $effect(() => {
    if (revision === seenRevision || soon) return;
    seenRevision = revision;
    soon = setTimeout(() => { soon = undefined; if (!document.hidden) void load(true); }, 8000);
  });
  onDestroy(() => clearTimeout(soon));

  const activity = $derived(parts.length ? mergeActivity(parts.map((p) => activityOf(p.raw))) : null);
  const personal = $derived(parts.find((p) => p.workspace === 'personal'));
  const allRuns = $derived(personal ? runsOf(personal.raw) : null);
  const runs = $derived(personal ? runsOf(personal.raw, { provider, runner }) : null);
  const providers = $derived([...new Set([...(allRuns?.rows.map(r => r.provider) ?? []), ...(provider ? [provider] : [])])].sort());
  const runners = $derived([...new Set([...(allRuns?.rows.map(r => r.runnerId || '__unrecorded') ?? []), ...(runner ? [runner] : [])])].sort());
  const runnerName = (id: string) => id === '__unrecorded' || !id ? 'Not recorded' : names.runners[id] || id;
  const projectName = (id: string) => names.projects[id] || id;
  const canRuns = $derived(runsAvailable(targets));
  const chartScope = $derived(`${where}/${projectKey}/${days}/${view}/${provider}/${runner}/${measure}`);
  const hasThroughput = $derived(!!activity?.days.some((d) => d.created || d.completed));
  const doneShare = $derived(activity && activity.created > 0 ? activity.completed / activity.created : null);
  const heat = $derived(activity?.days.map((d) => ({ date: d.date, value: d.created + d.completed })) ?? []);
    const outcomes: [string, string, string][] = [['completed', 'Normal exit', 'var(--ok)'], ['failed', 'Failed', 'var(--danger)'], ['stopped', 'Stopped', 'var(--accent)'], ['blocked', 'Blocked', 'var(--accent)'], ['waiting_for_user', 'Waiting', 'var(--warn)'], ['running', 'Running', 'var(--accent)']];
  const outcomeSlices = $derived(runs ? outcomes.map(([state, label, tone]) => ({ label, value: (runs.totals.outcomes as Record<string, number | undefined>)[state] ?? 0, tone })) : []);
  const usage = $derived(runs ? (measure === 'tokens' ? [
    { label: 'Input tokens', value: runs.totals.inputTokens, detail: `${runs.totals.inputRuns}/${runs.totals.attempts} runs reported input` },
    { label: 'Output tokens', value: runs.totals.outputTokens, detail: `${runs.totals.outputRuns}/${runs.totals.attempts} runs reported output` },
    { label: 'Cached tokens', value: runs.totals.cachedTokens, detail: `${runs.totals.cachedRuns}/${runs.totals.attempts} runs reported cached tokens` },
  ] : [
    { label: 'Estimated cost', value: runs.totals.estimatedCostUsd, formatted: measuredCost(runs.totals.estimatedCostUsd), detail: `${runs.totals.estimatedCostRuns}/${runs.totals.attempts} runs have estimates` },
    { label: 'Reported API cost', value: runs.totals.actualCostUsd, formatted: measuredCost(runs.totals.actualCostUsd), detail: `${runs.totals.actualCostRuns}/${runs.totals.attempts} runs reported cost`, tone: 'var(--warn)' },
  ]) : []);
</script>

<PageFrame id="analytics-h" title="Analytics" lede="How work is going in the selected project context.">
  <FilterBar busy={loading}>
    <label class="pick"><span>Period</span>
      <select bind:value={days} aria-label="Analytics period" data-testid="analytics-days">
        {#each RANGES as d (d)}<option value={d}>Last {d} days</option>{/each}
      </select>
    </label>
    <Segmented compact options={views.map((v) => ({ id: v, label: v }))} value={view} onchange={(v) => (view = v)} label="Analytics view" />
    {#if view === 'Runs and usage' && canRuns}
      <label class="pick"><span>Provider</span><select aria-label="Analytics agent provider" bind:value={provider}><option value="">All agent providers</option>{#each providers as id (id)}<option value={id}>{agentName(id)}</option>{/each}</select></label>
      <label class="pick"><span>Runner</span><select aria-label="Analytics runner" bind:value={runner}><option value="">All runners</option>{#each runners as id (id)}<option value={id}>{runnerName(id)}</option>{/each}</select></label>
      <label class="pick"><span>Measure</span><select aria-label="Usage measure" bind:value={measure}><option value="tokens">Tokens</option><option value="cost">API cost</option></select></label>
    {/if}
  </FilterBar>

  {#each failed as f (f.workspace)}
    <Notice>{nameOf(f.workspace)} could not be read: {f.message} Its figures are left out.</Notice>
  {/each}

  {#if !loaded}
    <p class="empty" role="status">Loading analytics…</p>
  {:else if !activity}
    <p class="empty">Nothing could be read. Check that Personal and your teams are connected.</p>
  {:else}
    <p class="muted stamp">{label} · the last {days} days{activity.end ? ` · as of ${new Date(asOf || activity.end).toLocaleString()}` : ''}</p>
    {#if activity.partial}
      <p class="notice" role="status">Some history is unavailable, so totals may be incomplete.</p>
    {/if}

    {#if view === 'Work'}
      <dl class="statistics">
        <StatTile label="Tasks created" value={activity.created} tone="var(--accent)" spark={activity.days.map((d) => d.created)} sparkLabel="Tasks created per day" />
        <StatTile label="Moved to Done" value={activity.completed} tone="var(--ok)" spark={activity.days.map((d) => d.completed)} sparkLabel="Tasks moved to Done per day" />
        <StatTile label={activity.medianSeconds === null ? 'Average task duration' : 'Median task duration'} value={analyticsDuration(activity.medianSeconds ?? activity.averageSeconds)} hint={activity.medianSeconds === null ? 'A median is shown for one workspace at a time' : `Average ${analyticsDuration(activity.averageSeconds)}`} tone="var(--block)" />
        <StatTile label="Duration samples" value={activity.durationSamples} hint="Tasks the duration is drawn from" tone="var(--warn)" />
      </dl>
      {#if hasThroughput}
        <div class="board">
          <Panel id="throughput-title" title="Created and completed over time" caption="Tasks per day, by UTC day" tone="var(--accent)" span={7}>
            <div class="series"><label class="pick"><span>Show</span><select bind:value={series} aria-label="Activity chart series"><option value="both">Created and Done</option><option value="created">Created</option><option value="completed">Moved to Done</option></select></label></div>
            <AnalyticsDaily days={activity.days} {series} scope={chartScope} />
          </Panel>
          <Panel title="Progress so far" caption="Running totals within this period; the band shows their difference" tone="var(--ok)" span={5}><LineProgress days={activity.days} {series} /></Panel>
          <Panel title="Done against created" caption="Tasks moved to Done for each one created" tone="var(--ok)" span={3}>
            <Ring value={doneShare} tone="var(--ok)" caption={doneShare === null ? 'nothing created' : 'Done / created'} label="Moves to Done divided by tasks created in this period" />
            <p class="muted tail">{activity.completed} done · {activity.created} created{activity.completed > activity.created ? '. More were finished than started: some were created before this period.' : '.'}</p>
          </Panel>
          <Panel title="Busiest days" caption="Created plus moved to Done, a square for each day" tone="var(--block)" span={5}><Heat days={heat} tone="var(--block)" label="Activity for each day of the period" /></Panel>
          <Panel title="Work categories" caption="Labels with the same name are counted together across workspaces" tone="var(--warn)" span={4}>
            <AnalyticsBars shares={false} scope={chartScope} label="Task activity by category" rows={activity.categories.map((c) => ({ label: c.name, value: c.completed, detail: `${c.created} created · ${c.completed} moved to Done`, tone: 'var(--warn)' }))} />
            {#if !activity.categories.length}<p class="muted">No task activity matches these categories.</p>{/if}
          </Panel>
        </div>
      {:else}
        <p class="empty">No task activity in this period. Try a longer period or another place.</p>
      {/if}
    {:else if !canRuns || !runs}
      <p class="notice" role="status" data-testid="analytics-runs-personal-only">
        {view === 'Outcomes' ? 'Personal outcome evidence' : 'Agent usage'} is available in Personal. Choose Personal to view it.
      </p>
    {:else if view === 'Outcomes' && allRuns}
      <dl class="statistics">
        <StatTile label="Accepted" value={allRuns.accepted} tone="var(--ok)" />
        <StatTile label="Rejected" value={allRuns.rejected} tone="var(--danger)" />
        <StatTile label="Controller-verified merges" value={allRuns.verifiedMerges} tone="var(--block)" />
        <StatTile label="Normal session ends" value={allRuns.normalSessionCompletions} tone="var(--accent)" />
      </dl>
      <p class="muted">For the selected Personal project and period. Assessments, moves to Done, session ends and verified merges are separate signals.</p>
      <div class="board">
        <Panel title="Recorded assessments" caption="Your decision on assessed runs" span={5} tone="var(--ok)">
          <AnalyticsBars shares={false} scope={chartScope} label="Accepted and rejected work" rows={[{ label: 'Accepted', value: allRuns.accepted }, { label: 'Rejected', value: allRuns.rejected }]} />
        </Panel>
        <Panel title="Completion evidence" caption="Separate counts, not steps of one process" span={7} tone="var(--accent)">
          <AnalyticsBars shares={false} scope={chartScope} label="Separate completion signals" rows={[{ label: 'Moved to Done', value: allRuns.completed }, { label: 'Normal session ends', value: allRuns.normalSessionCompletions }, { label: 'Controller-verified merges', value: allRuns.verifiedMerges }]} />
        </Panel>
      </div>
      <details class="disclosure"><summary>Completion details</summary><dl class="facts">
        <div><dt>Agent-reported task completion</dt><dd>{allRuns.agentReportedCompletion ?? 'Not recorded'}</dd></div>
        <div><dt>Independently verified task completion</dt><dd>{allRuns.verifiedCompletion ?? 'Not recorded'}</dd></div>
      </dl><p class="muted">A normal session exit does not establish that a task is done. Missing evidence stays unknown.</p></details>
    {:else}
      <dl class="statistics">
        <StatTile label="Run attempts" value={runs.totals.attempts} tone="var(--accent)" />
        <StatTile label="Retries" value={runs.totals.retries} hint={percent(runs.totals.retries, runs.totals.attempts) === null ? '' : `${percent(runs.totals.retries, runs.totals.attempts)}% of attempts`} tone="var(--warn)" />
        <StatTile label="Failed runs" value={(runs.totals.outcomes as Record<string, number | undefined>).failed ?? 0} tone="var(--danger)" />
        <StatTile label="Reported API cost" value={measuredCost(runs.totals.actualCostUsd)} tone="var(--warn)" />
      </dl>
      <p class="muted">Runs started in this period. A run that finished normally does not mean the task is done.</p>
      <details class="disclosure"><summary>Reporting details</summary><p class="muted">Execution figures come from Personal for the selected project context. Team coordination does not report agent usage. Missing reports stay unknown. {runs.totals.partialRuns} partial reports across {runs.totals.attempts} runs.</p></details>
      <div class="board">
        <Panel title="Run outcomes" caption="Where each attempt ended up" tone="var(--accent)" span={5}>
          {#if runs.totals.attempts}<Donut slices={outcomeSlices} centre={String(runs.totals.attempts)} caption={runs.totals.attempts === 1 ? 'run' : 'runs'} label="Run outcomes by count" />{:else}<p class="empty">No runs in this period.</p>{/if}
        </Panel>
        <Panel title={measure === 'tokens' ? 'Tokens' : 'API cost'} caption="What agents reported; unknown is shown as unknown" tone="var(--warn)" span={7}>
          <AnalyticsBars shares={false} scope={chartScope} label={measure === 'tokens' ? 'Token usage' : 'API cost in USD'} rows={usage} />
          <p class="muted tail">{measuredNumber(runs.totals.limitEvents)} usage-limit notices · {runs.totals.partialRuns} partial reports</p>
        </Panel>
        <Panel title="Attempts by agent" caption="Which runtime did the work" tone="var(--accent)" span={6}>
          <AnalyticsBars shares={false} scope={chartScope} label="Attempts by agent provider" rows={runs.providers.map((p) => ({ label: p.name ? agentName(p.name) : 'Not recorded', value: p.attempts, tone: 'var(--accent)' }))} />
          {#if !runs.providers.length}<p class="muted">No attempts to group.</p>{/if}
        </Panel>
        <Panel title="Attempts by runner" caption="Which machine ran the work" tone="var(--block)" span={6}>
          <AnalyticsBars shares={false} scope={chartScope} label="Attempts by runner" rows={sumBy(runs.rows, 'runnerId', r => r.attempts).map(r => ({ label: runnerName(r.key), value: r.value }))} />
        </Panel>
      </div>
      {#if provider || runner}<p class="muted">Daily usage trends are available with all providers and runners selected; the report does not split daily measurements by runner or provider.</p>
      {:else}<AnalyticsUsageDetails rows={runs.rows} days={runs.days} scope={chartScope} />{/if}
      <details class="disclosure"><summary>Breakdown</summary>
        <p class="muted">Configured models may differ from resolved models. Reported API costs and estimated equivalents are separate, never added together.</p>
        <!-- svelte-ignore a11y_no_noninteractive_tabindex (The execution evidence table needs a keyboard-scrollable region.) -->
        <div class="table-scroll" role="region" aria-label="Runner, provider and model execution breakdown" tabindex="0"><table><thead><tr>{#each ['Project', 'Runner', 'Agent provider', 'Configured model', 'Attempts / retries', 'Input / output tokens', 'API cost / estimated', 'Limit notices'] as heading (heading)}<th>{heading}</th>{/each}</tr></thead><tbody>
          {#each runs.rows as row (JSON.stringify([row.projectId, row.runnerId, row.provider, row.model]))}<tr>
            <td>{projectName(row.projectId)}</td><td>{runnerName(row.runnerId)}</td><td>{agentName(row.provider)}</td><td>{row.model || 'Agent default · unknown'}</td>
            <td>{row.attempts} / {row.retries}</td><td>{measuredNumber(row.inputTokens)} / {measuredNumber(row.outputTokens)}<small>{row.inputRuns} input, {row.outputRuns} output reports</small></td>
            <td>{measuredCost(row.actualCostUsd)} / {measuredCost(row.estimatedCostUsd)}<small>{row.actualCostRuns} actual, {row.estimatedCostRuns} estimated reports</small></td><td>{row.limitEvents}</td>
          </tr>{:else}<tr><td colspan="8">No execution evidence matches these filters.</td></tr>{/each}
        </tbody></table></div>
      </details>
    {/if}
  {/if}
</PageFrame>

<style>
  .table-scroll { overflow: auto; max-width: 100%; }
  .table-scroll:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  table { border-collapse: collapse; width: 100%; font-size: 12px; }
  th, td { padding: 10px; text-align: left; border-bottom: 1px solid var(--border); vertical-align: top; }
  th { color: var(--text-2); font-weight: 500; }
  td small { display: block; color: var(--text-2); }
  .series { margin-bottom: 10px; }
  .stamp { margin: 0; }
  .muted { color: var(--text-2); font-size: 12px; line-height: 1.6; }
  .tail { margin-top: 10px; text-align: center; }
  p { margin: 0; }
  .empty { color: var(--text-2); }
  .statistics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; margin: 0; }
  .board { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 14px; align-items: stretch; }
  .facts { margin: 0; }
  .facts div { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; padding: 7px 0; border-bottom: 1px solid var(--border); }
  .facts div:last-child { border-bottom: 0; }
  dt { color: var(--text-2); font-size: 12px; }
  dd { margin: 0; font-family: var(--mono); font-size: 12px; text-align: right; }
  @media (max-width: 1100px) { .statistics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .board { grid-template-columns: 1fr; } }
</style>
