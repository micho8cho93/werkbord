<script lang="ts">
  import { api } from './api';
  import { app } from './state.svelte';
  import type { Run } from './types';
  let {run}: {run:Run} = $props();
  let error=$state(''); let busy=$state(false);
  const runner=$derived(app.runners.find(r=>r.id===run.runnerId));
  async function assess(acceptance:'accepted'|'rejected') {
    busy=true; error='';
    try { const updated=await api.assessRun(run,acceptance); run=updated; app.refreshOverview(); }
    catch(e) {error=e instanceof Error?e.message:String(e);}
    finally {busy=false;}
  }
</script>
<div class="usage">
  <p class="muted">Runner: <strong>{runner?.name ?? (run.remote ? run.runnerId : 'This computer')}</strong>{run.remote && runner && !runner.online && !run.endedAt ? ' · Offline; execution ownership retained' : ''}</p>
  {#if run.branch}<p class="muted">Branch: <code>{run.branch}</code>{#if run.remote} · Retained on its runner; commit and push there to share for review.{/if}</p>{/if}
  <p class="muted">Input tokens: {run.usage?.inputTokens?.toLocaleString() ?? 'Unknown'} · Output tokens: {run.usage?.outputTokens?.toLocaleString() ?? 'Unknown'}</p>
  <p class="muted">{#if run.usage?.costUsd !== undefined}{run.usage.costKind==='actual_api'?'Known API cost':'API-equivalent estimate'}: {new Intl.NumberFormat(undefined,{style:'currency',currency:'USD'}).format(run.usage.costUsd)}{:else}Usage only · cost unknown{/if}</p>
  {#if run.usage?.source}<p class="muted small">Source: {run.usage.source}</p>{/if}
  {#if run.endedAt}<div class="assessment"><span>Human assessment: {run.usage?.acceptance ?? 'Not recorded'}</span><button class="btn small" disabled={busy} onclick={()=>assess('accepted')}>Accept work</button><button class="btn small" disabled={busy} onclick={()=>assess('rejected')}>Reject work</button></div>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</div>
<style>
  .usage { display: grid; gap: 6px; font-size: .85rem; } p { margin: 0; } code { overflow-wrap: anywhere; } .small { font-size: .8rem; } .assessment { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 4px; }
</style>
