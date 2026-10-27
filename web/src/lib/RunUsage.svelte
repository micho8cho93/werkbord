<script lang="ts">
  import { app } from './state.svelte';
  import type { Run } from './types';
  import RunAssessment from './RunAssessment.svelte';
  let {run, showAssessment = true}: {run:Run; showAssessment?: boolean} = $props();
  const runner=$derived(app.runners.find(r=>r.id===run.runnerId));
</script>
<div class="usage">
  <p class="muted">Runner: <strong>{runner?.name ?? (run.remote ? run.runnerId : 'This computer')}</strong>{run.remote && runner && !runner.online && !run.endedAt ? ' · Offline. This run still belongs to this runner.' : ''}</p>
  {#if run.branch}<p class="muted">Branch: <code>{run.branch}</code>{#if run.remote} · Retained on its runner; commit and push there to share for review.{/if}</p>{/if}
  {#if run.usage?.partial}<p class="muted" role="status">Partial usage: this session was resumed; the total may be incomplete.</p>{/if}
  <p class="muted">Input tokens: {run.usage?.inputTokens?.toLocaleString() ?? 'Unknown'} · Output tokens: {run.usage?.outputTokens?.toLocaleString() ?? 'Unknown'}</p>
  <p class="muted">{#if run.usage?.costUsd !== undefined}{run.usage.costKind==='actual_api'?'Known API cost':'API-equivalent estimate (not a bill)'}: {new Intl.NumberFormat(undefined,{style:'currency',currency:'USD'}).format(run.usage.costUsd)}{:else}Usage only · cost unknown{/if}</p>
  {#if run.usage?.source}<p class="muted small">Source: {run.usage.source}</p>{/if}
  {#if showAssessment && run.endedAt}<RunAssessment {run} />{/if}
</div>
<style>
  .usage { display: grid; gap: 6px; font-size: .85rem; } p { margin: 0; } code { overflow-wrap: anywhere; } .small { font-size: .8rem; }
</style>
