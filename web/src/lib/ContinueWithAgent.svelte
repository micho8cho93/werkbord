<script lang="ts">
  import Sheet from './Sheet.svelte';
  import { api } from './api';
  import { agentName } from './format';
  import { app } from './state.svelte';
  import { approveHold, continuityOf, otherProviders } from './continuity';
  import type { ProjectScope } from './scope.svelte';
  import type { Run } from './types';

  // "Continue with another agent?" Nothing is dispatched until Approve, and Approve is available only once the previous run
  // is confirmed stopped. What the handoff says is agent-written, and is labelled so.
  let { run, scope, onclose, onreview }: { run: Run; scope: ProjectScope; onclose: () => void; onreview: () => void } = $props();
  const c = $derived(continuityOf(run, app.connection === 'live' && (!run.remote || !!app.runners.find(r => r.id === run.runnerId)?.online)));
  const options = $derived(otherProviders(app.agents, run.agentId));
  let chosen = $state('');
  const provider = $derived(chosen && options.some(a => a.id === chosen) ? chosen : options[0]?.id ?? '');
  const name = (id: string) => agentName(app.agents, id);
  const h = $derived(run.handoff);
  const open = $derived([...(h?.knownIssues ?? []), ...(h?.blockers ?? []), ...(h?.questions ?? [])]);
  const hold = $derived(approveHold(c, !!h, provider));
  let busy = $state(false);
  let error = $state('');
  async function approve() {
    if (hold || busy) return;
    busy = true; error = '';
    try {
      const next = await api.continueRun(run, { agentId: provider }, `Continue after ${name(run.agentId)} stopped`, '', '');
      scope.upsertRun(next);
      onclose();
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
</script>

<Sheet title="Continue with another agent?" {onclose} width="36rem">
  <div class="body" data-testid="continue-dialog">
    <p><span class="chip">Needs review</span> {c.headline}</p>
    <p class="muted">Your work is retained: the branch, the changes and what was recorded. Nothing has been dispatched, and choosing another agent does not replay the previous one's commands.</p>
    <dl>
      <dt>Completed by <span class="tag">agent-reported</span></dt><dd>{h?.summary || h?.results || 'No handoff has been captured yet.'}</dd>
      <dt>Still to verify <span class="tag">agent-reported</span></dt><dd>{open.length ? open.join('\n') : 'Nothing reported.'}</dd>
      <dt>Current branch</dt><dd><code>{run.branch || 'No branch recorded'}</code></dd>
      <dt>Proposed provider</dt>
      <dd>{#if options.length}<select class="input" aria-label="Proposed provider" bind:value={chosen}>{#each options as a (a.id)}<option value={a.id} selected={a.id === provider}>{a.name}</option>{/each}</select>{:else}None available{/if}</dd>
    </dl>
    <p class="caution" role="note">Confirm the previous run has stopped, and review anything it did outside the repository, before the next agent starts.{#if hold} <strong>{hold}</strong>{/if}</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </div>
  {#snippet footer()}
    <button class="btn" onclick={onclose}>Wait for {name(run.agentId)}</button>
    <button class="btn" onclick={() => { onreview(); onclose(); }}>Review handoff</button>
    <button class="btn primary" disabled={!!hold || busy} aria-describedby={hold ? 'continue-hold' : undefined} onclick={approve} data-testid="continue-approve">{busy ? 'Starting…' : provider ? `Approve ${name(provider)}` : 'Approve'}<span class="basis"> · billed to your {provider ? name(provider) : 'provider'} account</span></button>
    {#if hold}<span id="continue-hold" class="visually-hidden">{hold}</span>{/if}
  {/snippet}
</Sheet>

<style>
  .body{display:grid;gap:14px;min-width:0;}p{margin:0;}.chip{padding:1px 8px;border-radius:999px;font-size:12px;background:color-mix(in srgb,var(--warn) 14%,var(--surface));color:var(--text);margin-right:6px;}
  dl{display:grid;grid-template-columns:minmax(0,10rem) minmax(0,1fr);gap:10px 16px;margin:0;}dt{color:var(--text-2);font-size:13px;}dd{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;}
  .tag{font-size:11px;padding:0 6px;border-radius:999px;background:var(--surface-2);margin-left:4px;}.caution{padding:10px 12px;border:1px solid var(--border);border-radius:6px;background:var(--surface-2);font-size:13px;}.basis{font-weight:400;font-size:12px;opacity:.85;}
  @media(max-width:560px){dl{grid-template-columns:1fr;}}
</style>
