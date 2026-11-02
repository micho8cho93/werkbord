<script lang="ts">
  import Sheet from './Sheet.svelte';
  import { api } from './api';
  import { agentName } from './format';
  import { app } from './state.svelte';
  import { approveHold, continuityOf, continueAgents } from './continuity';
  import { fallbackLine, type FallbackView } from './fallback';
  import type { ProjectScope } from './scope.svelte';
  import type { Run } from './types';

  // "Continue this work": the one way to carry on from a run that stopped, failed or got stuck. It starts a new session from what the
  // run recorded, with the agent you pick, and does nothing until Approve. A session a limit left open and idle is ended by the same
  // step. What the handoff says is agent-written, and is labelled so.
  let { run, scope, onclose, onreview }: { run: Run; scope: ProjectScope; onclose: () => void; onreview: () => void } = $props();
  const c = $derived(continuityOf(run, app.connection === 'live' && (!run.remote || !!app.runners.find(r => r.id === run.runnerId)?.online)));
  const options = $derived(continueAgents(app.agents, run.agentId, c.providerLimit));
  let chosen = $state('');
  const provider = $derived(chosen && options.some(a => a.id === chosen) ? chosen : options[0]?.id ?? '');
  const name = (id: string) => agentName(app.agents, id);
  const h = $derived(run.handoff);
  const open = $derived([...(h?.knownIssues ?? []), ...(h?.blockers ?? []), ...(h?.questions ?? [])]);
  const hold = $derived(approveHold(c, provider));
  let busy = $state(false);
  let error = $state('');
  let auto = $state(false);
  let fallback = $state<FallbackView | null>(null);
  $effect(() => {
    let live = true;
    api.fallback(run.projectId).then(v => { if (live) fallback = v; }, () => {});
    return () => { live = false; };
  });
  // Offered when a limit is what stopped the work: so the next one needs no dialog.
  const offerAuto = $derived(c.providerLimit && !!fallback && !(fallback.enabled && fallback.providers.includes(provider)));
  const earlier = $derived(fallback?.ledger.find(e => e.runId === run.id && e.result === 'declined'));
  async function approve() {
    if (hold || busy) return;
    busy = true; error = '';
    try {
      const next = await api.continueRun(run, { agentId: provider }, 'continue', '', '', c.stopFirst);
      scope.upsertRun(next);
      if (auto && offerAuto && fallback) {
        const { maxSwitches, dailyLimit } = fallback;
        await api.setFallback(run.projectId, { enabled: true, providers: [...fallback.providers.filter(id => id !== provider), provider], maxSwitches, dailyLimit }).catch(() => {});
      }
      onclose();
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
</script>

<Sheet title="Continue this work" {onclose} width="36rem">
  <div class="body" data-testid="continue-dialog">
    <p><span class="chip">Needs review</span> {c.headline}</p>
    <p class="muted">Your work is retained: the branch, the changes and what was recorded. {#if c.stopFirst}This session is still open but idle, so continuing ends it first. {/if}Nothing is replayed: the next agent starts a new conversation from the handoff.</p>
    {#if earlier}<p class="muted" data-testid="fallback-earlier">Automatic fallback looked at this and did not continue it. {fallbackLine(earlier, name)}</p>{/if}
    <dl>
      <dt>Completed by <span class="tag">agent-reported</span></dt><dd>{h?.summary || h?.results || 'No handoff has been recorded yet.'}</dd>
      <dt>Still to verify <span class="tag">agent-reported</span></dt><dd>{open.length ? open.join('\n') : 'Nothing reported.'}</dd>
      <dt>Current branch</dt><dd><code>{run.branch || 'No branch recorded'}</code></dd>
      <dt>Continue with</dt>
      <dd>{#if options.length}<select class="input" aria-label="Continue with" bind:value={chosen}>{#each options as a (a.id)}<option value={a.id} selected={a.id === provider}>{a.name}</option>{/each}</select>{:else}No other agent is available{/if}</dd>
    </dl>
    {#if offerAuto}<label class="auto"><input type="checkbox" bind:checked={auto} data-testid="continue-auto" />Do this automatically in this project the next time a rate limit or an empty allowance stops an agent</label>{/if}
    {#if hold}<p class="caution" role="note"><strong>{hold}</strong></p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </div>
  {#snippet footer()}
    <button class="btn" onclick={onclose}>Not now</button>
    <button class="btn" onclick={() => { onreview(); onclose(); }}>Review handoff</button>
    <button class="btn primary" disabled={!!hold || busy} aria-describedby={hold ? 'continue-hold' : undefined} onclick={approve} data-testid="continue-approve">{busy ? 'Starting…' : provider ? `Continue with ${name(provider)}` : 'Continue'}<span class="basis"> · billed to your {provider ? name(provider) : 'provider'} account</span></button>
    {#if hold}<span id="continue-hold" class="visually-hidden">{hold}</span>{/if}
  {/snippet}
</Sheet>

<style>
  .body{display:grid;gap:14px;min-width:0;}p{margin:0;}.chip{padding:1px 8px;border-radius:999px;font-size:12px;background:color-mix(in srgb,var(--warn) 14%,var(--surface));color:var(--text);margin-right:6px;}
  dl{display:grid;grid-template-columns:minmax(0,10rem) minmax(0,1fr);gap:10px 16px;margin:0;}dt{color:var(--text-2);font-size:13px;}dd{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;}
  .tag{font-size:11px;padding:0 6px;border-radius:999px;background:var(--surface-2);margin-left:4px;}.caution{padding:10px 12px;border:1px solid var(--border);border-radius:6px;background:var(--surface-2);font-size:13px;}.basis{font-weight:400;font-size:12px;opacity:.85;}
  .auto{display:flex;gap:10px;align-items:flex-start;font-size:13px;}
  @media(max-width:560px){dl{grid-template-columns:1fr;}}
</style>
