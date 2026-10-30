<script lang="ts">
  import AnalyticsBars from './AnalyticsBars.svelte';
  import { executionTotals, measuredCost, measuredNumber, type AnalyticsExecution, type AnalyticsReport } from './analytics';
  let { rows, days, scope, showModels = true }: { showModels?: boolean; rows: AnalyticsExecution[]; days: AnalyticsReport['days']; scope: string } = $props();
  const models = $derived.by(() => {
    const groups: Record<string, AnalyticsExecution[]> = {};
    for (const row of rows) {
      const key = JSON.stringify([row.provider, row.model]);
      (groups[key] ??= []).push(row);
    }
    return Object.entries(groups).map(([key, group]) => ({ key, provider: group[0].provider, model: group[0].model, ...executionTotals(group) }));
  });
</script>

<section aria-labelledby="usage-trend-title">
  <h3 id="usage-trend-title">Usage trends</h3>
  <p class="muted">Reported input tokens by run start day (UTC). These are the latest totals for runs started that day, not daily spend. Missing measurements remain unavailable.</p>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex (The daily usage list needs a keyboard-focusable vertical scroll region.) -->
  <div class="trend" role="region" aria-label="Usage by run start day" tabindex="0">
  <AnalyticsBars shares={false} {scope} label="Reported input tokens by run start day" rows={days.map(day => ({ label: day.date, value: day.inputTokens ?? null, detail: `${day.attempts ?? 0} attempts · ${day.inputRuns ?? 0} input reports` }))} />
  </div>
  {#if !days.length}<p class="muted">Usage history is unavailable in this report.</p>{/if}
</section>
{#if showModels}<details>
  <summary>Provider and model usage</summary>
  <p class="muted">Configured models may differ from the provider’s resolved model. Actual provider-reported costs and estimates are separate figures; they are never added together. Subscription charges and remaining quota are unavailable.</p>
  <div class="breakdowns">
    {#each models as row (row.key)}
      <section>
        <h4>{row.provider || 'Provider not recorded'} · {row.model || 'Default model · unknown'}</h4>
        <dl>
          <div><dt>Attempts / retries</dt><dd>{row.attempts} / {row.retries}</dd></div>
          <div><dt>Input / output tokens</dt><dd>{measuredNumber(row.inputTokens)} / {measuredNumber(row.outputTokens)}</dd></div>
          <div><dt>Provider-reported cost</dt><dd>{measuredCost(row.actualCostUsd)} ({row.actualCostRuns} reports)</dd></div>
          <div><dt>Estimated cost</dt><dd>{measuredCost(row.estimatedCostUsd)} ({row.estimatedCostRuns} reports)</dd></div>
        </dl>
      </section>
    {:else}<p class="muted">No execution evidence in this view.</p>{/each}
  </div>
</details>{/if}

<style>
  h3, h4 { margin: 0 0 8px; font-size: 14px; }
  .muted, dt { color: var(--text-2); font-size: 12px; line-height: 1.6; }
  .trend { max-height: 320px; overflow: auto; padding: 4px; }
  .breakdowns { display: grid; gap: 16px; }
  dl { margin: 0; }
  dl div { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; padding: 6px 0; border-bottom: 1px solid var(--border); }
  dd { margin: 0; font-size: 12px; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
  summary { cursor: pointer; padding-block: 12px; }
</style>
