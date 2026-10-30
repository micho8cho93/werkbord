<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import AnalyticsBars from '../lib/AnalyticsBars.svelte';
  import AnalyticsDaily from '../lib/AnalyticsDaily.svelte';
  import Donut from '../lib/charts/Donut.svelte';
  import Heat from '../lib/charts/Heat.svelte';
  import LineProgress from '../lib/charts/LineProgress.svelte';
  import Panel from '../lib/charts/Panel.svelte';
  import Ring from '../lib/charts/Ring.svelte';
  import StatTile from '../lib/charts/StatTile.svelte';
  import { analyticsDuration, measuredCost, measuredNumber, percent } from '../lib/analytics';
  import { activityOf, mergeActivity, RANGES, runsAvailable, runsOf } from './analytics';
  import FilterBar from './FilterBar.svelte';
  import Notice from './Notice.svelte';
  import PageFrame from './PageFrame.svelte';
  import Segmented from '../lib/Segmented.svelte';
  import { model } from './model.svelte';
  import { ask } from './native';

  // One Analytics page for the whole window. Choose where to look (Personal and every team, Personal alone, or one team), a project
  // within that, and a period; what each workspace reports about the work itself is merged into one picture. What an agent did (runs,
  // tokens, cost) is reported by Personal alone and has a view of its own.

  const views = ['Work', 'Runs and usage'] as const;
  type View = (typeof views)[number];
  let view = $state<View>('Work');
  const where = $derived(model.selectedProject?.workspace ?? 'all');
  const projectKey = $derived(model.selectedProject?.project ?? '');
  let days = $state<number>(30);
  let series = $state('both');
  const items = $derived(model.view?.items);
  const targets = $derived((items ?? []).filter(i => i.state === 'ready' && (!model.selectedProject || i.id === model.selectedProject.workspace))
    .map(i => ({ workspace: i.id, project: model.selectedProject?.project ?? '' })));
  const nameOf = (id: string): string => items?.find(i => i.id === id)?.name ?? id;

  let parts = $state<{ workspace: string; raw: unknown }[]>([]);
  let failed = $state<{ workspace: string; message: string }[]>([]);
  let loading = $state(true);
  let loaded = $state(false);
  let requestId = 0;

  async function load(): Promise<void> {
    const id = ++requestId;
    const wanted = untrack(() => ({ targets, days }));
    loading = true;
    const results = await Promise.all(
      wanted.targets.map(async (t) => {
        try {
          return { workspace: t.workspace, raw: await ask((a) => a.Analytics(t.workspace, t.project, wanted.days)) };
        } catch (e) {
          return { workspace: t.workspace, message: e instanceof Error ? e.message : String(e) };
        }
      }),
    );
    if (id !== requestId) return;
    parts = results.filter((r): r is { workspace: string; raw: unknown } => 'raw' in r);
    failed = results.filter((r): r is { workspace: string; message: string } => 'message' in r);
    loading = false;
    loaded = true;
  }

  $effect(() => {
    void [where, projectKey, days, targets.map(t => t.workspace).join()];
    parts = []; failed = []; loaded = false;
    ++requestId;
    const timer = setTimeout(() => void load(), 120);
    return () => clearTimeout(timer);
  });
  onMount(() => {
    const timer = setInterval(() => { if (!document.hidden) void load(); }, 30000);
    return () => clearInterval(timer);
  });

  const activity = $derived(parts.length ? mergeActivity(parts.map((p) => activityOf(p.raw))) : null);
  const personal = $derived(parts.find((p) => p.workspace === 'personal'));
  const runs = $derived(personal ? runsOf(personal.raw) : null);
  const canRuns = $derived(runsAvailable(targets));
  const chartScope = $derived(`${where}/${projectKey}/${days}`);
  const hasThroughput = $derived(!!activity?.days.some((d) => d.created || d.completed));
  const doneShare = $derived(activity && activity.created > 0 ? activity.completed / activity.created : null);
  const heat = $derived(activity?.days.map((d) => ({ date: d.date, value: d.created + d.completed })) ?? []);
  const describe = $derived(model.contextLabel);
  const outcomes: [string, string, string][] = [['completed', 'Normal exit', 'var(--ok)'], ['failed', 'Failed', 'var(--danger)'], ['stopped', 'Stopped', 'var(--accent)'], ['blocked', 'Blocked', 'var(--accent)'], ['waiting_for_user', 'Waiting', 'var(--warn)'], ['running', 'Running', 'var(--accent)']];
  const outcomeSlices = $derived(runs ? outcomes.map(([state, label, tone]) => ({ label, value: (runs.totals.outcomes as Record<string, number | undefined>)[state] ?? 0, tone })) : []);
  const usage = $derived(runs ? [
    { label: 'Input tokens', value: runs.totals.inputTokens, detail: `${runs.totals.inputRuns}/${runs.totals.attempts} runs reported input` },
    { label: 'Output tokens', value: runs.totals.outputTokens, detail: `${runs.totals.outputRuns}/${runs.totals.attempts} runs reported output` },
    { label: 'Reported API cost', value: runs.totals.actualCostUsd, formatted: measuredCost(runs.totals.actualCostUsd), detail: `${runs.totals.actualCostRuns}/${runs.totals.attempts} runs reported cost`, tone: 'var(--warn)' },
  ] : []);
</script>

<PageFrame id="analytics-h" title="Analytics" lede="How work is going in the selected project context.">
  <FilterBar busy={loading}>
    <label class="pick"><span>Period</span>
      <select bind:value={days} aria-label="Analytics period" data-testid="analytics-days">
        {#each RANGES as d (d)}<option value={d}>Last {d} days</option>{/each}
      </select>
    </label>
    <Segmented compact options={views.map((v) => ({ id: v, label: v }))} value={view} onchange={(v) => (view = v)} label="Analytics view" />
    {#if loading}<span class="muted" role="status">Updating…</span>{/if}
  </FilterBar>

  {#each failed as f (f.workspace)}
    <Notice>{nameOf(f.workspace)} could not be read: {f.message} Its figures are left out.</Notice>
  {/each}

  {#if !loaded}
    <p class="empty" role="status">Loading analytics…</p>
  {:else if !activity}
    <p class="empty">Nothing could be read. Check that Personal and your teams are connected.</p>
  {:else}
    <p class="muted stamp">{describe} · the last {days} days{activity.end ? ` · as of ${new Date(activity.end).toLocaleString()}` : ''}</p>
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
        Agent usage is available in Personal. Choose Personal to view it.
      </p>
    {:else}
      <dl class="statistics">
        <StatTile label="Run attempts" value={runs.totals.attempts} tone="var(--accent)" />
        <StatTile label="Retries" value={runs.totals.retries} hint={percent(runs.totals.retries, runs.totals.attempts) === null ? '' : `${percent(runs.totals.retries, runs.totals.attempts)}% of attempts`} tone="var(--warn)" />
        <StatTile label="Failed runs" value={(runs.totals.outcomes as Record<string, number | undefined>).failed ?? 0} tone="var(--danger)" />
        <StatTile label="Reported API cost" value={measuredCost(runs.totals.actualCostUsd)} tone="var(--warn)" />
      </dl>
      <p class="muted">Runs started in this period. A run that finished normally does not mean the task is done.</p>
      <details class="disclosure"><summary>Reporting details</summary><p class="muted">These figures come from Personal only, whatever place is chosen, and show what agents reported. Missing reports stay unknown. {runs.totals.partialRuns} partial reports across {runs.totals.attempts} runs.</p></details>
      <div class="board">
        <Panel title="Run outcomes" caption="Where each attempt ended up" tone="var(--accent)" span={5}>
          {#if runs.totals.attempts}<Donut slices={outcomeSlices} centre={String(runs.totals.attempts)} caption={runs.totals.attempts === 1 ? 'run' : 'runs'} label="Run outcomes by count" />{:else}<p class="empty">No runs in this period.</p>{/if}
        </Panel>
        <Panel title="Tokens and cost" caption="What agents reported; unknown is shown as unknown" tone="var(--warn)" span={7}>
          <AnalyticsBars shares={false} scope={chartScope} label="Token usage and cost" rows={usage} />
          <p class="muted tail">{measuredNumber(runs.totals.limitEvents)} usage-limit notices · {runs.totals.partialRuns} partial reports</p>
        </Panel>
        <Panel title="Attempts by agent" caption="Which runtime did the work" tone="var(--accent)" span={6}>
          <AnalyticsBars shares={false} scope={chartScope} label="Attempts by agent provider" rows={runs.providers.map((p) => ({ label: p.name || 'Not recorded', value: p.attempts, tone: 'var(--accent)' }))} />
          {#if !runs.providers.length}<p class="muted">No attempts to group.</p>{/if}
        </Panel>
        <Panel title="Your decisions" caption="Assessed runs you accepted or rejected, and verified merges" tone="var(--ok)" span={6}>
          <dl class="facts"><div><dt>Accepted</dt><dd>{runs.accepted}</dd></div><div><dt>Rejected</dt><dd>{runs.rejected}</dd></div><div><dt>Controller-verified merges</dt><dd>{runs.verifiedMerges}</dd></div><div><dt>Currently blocked / stopped</dt><dd>{runs.blocked} / {runs.stopped}</dd></div></dl>
        </Panel>
      </div>
    {/if}
  {/if}
</PageFrame>

<style>
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
