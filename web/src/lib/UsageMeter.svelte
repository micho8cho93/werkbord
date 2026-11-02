<script lang="ts">
  import AgentMark from './AgentMark.svelte';
  import { agentName } from './format';
  import { app } from './state.svelte';
  import { easesLabel, hasUsage, pressure, windowLine, type UsageWindow } from './usage';
  import { usageStore } from './usage.svelte';

  // What the runs Werkbord started have used in the last five hours and seven days, per agent, against budgets you set. Measured here,
  // never what a provider has left: it does not say, so this does not guess. It is yours alone and is never part of a Team report.
  let { settingsHref = '', bare = false }: { settingsHref?: string; bare?: boolean } = $props();
  $effect(() => {
    void app.analyticsRevision;
    void usageStore.refresh();
    const t = setInterval(() => void usageStore.refresh(), 60000);
    return () => clearInterval(t);
  });
  const meter = $derived(usageStore.view?.meter);
  const saver = $derived(meter?.mode === 'saver');
  const name = (id: string) => agentName(app.agents, id);
  const rows = $derived((w: UsageWindow) => ({ line: windowLine(w), eases: easesLabel(w.easesAt, app.now), tone: pressure(w, meter?.saverAt ?? 80), value: Math.min(w.percent ?? 0, 100) }));
</script>

{#if meter && hasUsage(meter)}
  <section class="usage" data-testid="usage-meter" aria-label="Usage">
    {#if !bare}<header>
      <h3>Usage</h3>
      <span class="mode" data-on={saver} data-testid="usage-mode">{saver ? `Usage saver on · holds automatic work at ${meter.saverAt}%` : 'Measuring only'}</span>
      {#if settingsHref}<a class="set" href={settingsHref}>Budgets</a>{/if}
    </header>{/if}
    <ul>
      {#each meter.agents as a (a.agent)}
        {@const five = rows(a.fiveHour)}{@const week = rows(a.weekly)}
        <li data-agent={a.agent} data-held={a.held ? 'true' : 'false'}>
          <span class="who"><AgentMark agentId={a.agent} name={name(a.agent)} decorative /><strong>{name(a.agent)}</strong>{#if a.held}<span class="held" data-testid="usage-held">Automatic work waits</span>{/if}</span>
          <div class="win"><span class="label">Last 5 hours</span>
            {#if a.fiveHour.budget > 0}<progress class="bar" data-tone={five.tone} max="100" value={five.value} aria-label="{name(a.agent)}, last 5 hours: {five.line}"></progress>{/if}
            <span class="line">{five.line}{#if five.eases && a.fiveHour.runs} · {five.eases}{/if}</span></div>
          <div class="win"><span class="label">Last 7 days</span>
            {#if a.weekly.budget > 0}<progress class="bar" data-tone={week.tone} max="100" value={week.value} aria-label="{name(a.agent)}, last 7 days: {week.line}"></progress>{/if}
            <span class="line">{week.line}</span></div>
        </li>
      {/each}
    </ul>
    <p class="foot">Counted from runs started in Werkbord that reported tokens. Work done in a provider's own app is not included, and a provider does not tell Werkbord what is left of a plan, so percent is of the budget you set.</p>
  </section>
{/if}

<style>
  .usage { display: grid; gap: 10px; padding: 12px 14px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); }
  header { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
  h3 { margin: 0; font-size: 14px; }
  .mode { font-size: 12px; color: var(--text-2); }
  .mode[data-on='true'] { color: var(--text); }
  .set { margin-left: auto; font-size: 12px; }
  ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 12px; }
  li { display: grid; grid-template-columns: minmax(9rem, 14rem) 1fr 1fr; gap: 6px 18px; align-items: start; }
  .who { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
  .held { font-size: 11px; padding: 1px 8px; border-radius: 999px; background: color-mix(in srgb, var(--warn) 14%, var(--surface)); }
  .win { display: grid; gap: 4px; min-width: 0; }
  .label { font-size: 11px; color: var(--text-2); text-transform: uppercase; letter-spacing: .04em; }
  .line { font-size: 12px; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
  .bar { width: 100%; height: 6px; appearance: none; border: 0; border-radius: 6px; background: var(--surface-2); accent-color: var(--ok); }
  .bar::-webkit-progress-bar { background: var(--surface-2); border-radius: 6px; }
  .bar::-webkit-progress-value { background: var(--ok); border-radius: 6px; }
  .bar::-moz-progress-bar { background: var(--ok); border-radius: 6px; }
  .bar[data-tone='near']::-webkit-progress-value { background: var(--warn); } .bar[data-tone='near']::-moz-progress-bar { background: var(--warn); }
  .bar[data-tone='over']::-webkit-progress-value { background: var(--danger); } .bar[data-tone='over']::-moz-progress-bar { background: var(--danger); }
  .foot { margin: 0; font-size: 12px; color: var(--text-2); line-height: 1.5; }
  @media (max-width: 760px) { li { grid-template-columns: 1fr; } }
</style>
