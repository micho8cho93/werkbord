<script lang="ts">
  import { model } from './model.svelte';
  import { recap } from './catchup';
  import { catchUpSession } from './catchup-session.svelte';
  import type { Overview } from './types';
  let { overview, waiting }: { overview: Overview | null; waiting: number } = $props();
  const previous = $derived(catchUpSession.previous);
  const through = $derived(catchUpSession.through);
  let expanded = $state(false);
  let explanationError = $state('');
  const result = $derived(overview && previous ? recap(overview, previous, through) : null);
  const completed = $derived(result?.items.filter(i => i.change.kind === 'completed').length ?? 0);
  const merged = $derived(result?.items.filter(i => i.change.kind === 'pr_merged').length ?? 0);
  const elapsed = $derived(previous ? Math.max(0, Math.floor((through - previous.at) / 86_400_000)) : 0);
  async function explain() {
    // Refresh before explanation; never send cached Team content after permission changes.
    if (!await model.refreshOverview()) { explanationError = 'Recent activity could not be refreshed. Retry before sharing it with Salie.'; return; }
    explanationError = '';
    const current = model.overview && previous ? recap(model.overview, previous, through) : null;
    const evidence = current?.items.slice(0, 30).map(i => `- ${i.change.kind === 'completed' ? 'Completed' : 'Merged pull request'}: ${i.change.title} (${i.name}, ${new Date(i.change.at).toLocaleString()}). Source: ${i.change.href}`) ?? [];
    const coverage = [current?.incomplete ? 'Some history is outside the available coverage.' : '', current?.unavailable.length ? `Unavailable workspaces: ${current.unavailable.join(', ')}.` : '', (current?.items.length ?? 0) > evidence.length ? `Showing the first ${evidence.length} of ${current?.items.length} recorded events.` : ''].filter(Boolean).join(' ');
    await model.explainCatchUp(`Catch me up on the work recorded since ${new Date(previous!.at).toLocaleString()}. Explain what these facts establish and link to the source records. Say where the evidence is incomplete.\n${coverage}\n\n${evidence.join('\n') || 'No completion or pull-request merge events were recorded in this interval.'}`);
  }
</script>

<section class="catchup" aria-labelledby="catchup-title">
  <div class="heading"><h2 id="catchup-title">Since you were last here</h2>{#if previous}<span class="muted">{elapsed === 0 ? 'Earlier today' : `${elapsed} ${elapsed === 1 ? 'day' : 'days'}`}</span>{/if}</div>
  {#if explanationError}<p role="alert">{explanationError}</p>{/if}
  {#if !previous}<p class="muted">Your next visit will show what changed while you were away.</p>
  {:else if result}
    <div class="facts">
      <button class="btn small quiet" onclick={() => expanded = !expanded} aria-expanded={expanded}>{merged} pull {merged === 1 ? 'request' : 'requests'} merged · {completed} {completed === 1 ? 'item' : 'items'} completed</button>
      <button class="btn small quiet" onclick={() => { const heading = document.getElementById('hub-needs-h'); heading?.scrollIntoView({ block: 'nearest' }); heading?.focus(); }}>{waiting} {waiting === 1 ? 'item needs' : 'items need'} attention now</button>
      {#if !model.remotePersonal}<button class="btn small" onclick={() => { void explain(); }}>Catch me up with Salie</button>{/if}
    </div>
    {#if result.incomplete || result.unavailable.length}<p class="muted">{result.incomplete ? 'Some history is outside the available coverage. ' : ''}{result.unavailable.length ? `Unavailable: ${result.unavailable.join(', ')}.` : ''}</p>{/if}
    {#if expanded}<ul>{#each result.items as row (row.change.key)}<li><button class="btn small quiet" onclick={() => model.open(row.workspace, row.change.href)}>{row.change.title}</button><span class="muted">{row.name} · {row.change.kind === 'completed' ? 'completed' : 'PR merged'}</span></li>{/each}</ul>{/if}
  {:else}<p class="muted">Loading recent activity…</p>{/if}
</section>
<style>
  .catchup { border-block: 1px solid var(--border); padding-block: 12px; flex: none; }
  .heading, .facts { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; }
  h2, p { margin: 0; } h2 { font-size: 14px; } p { font-size: 13px; margin-top: 6px; }
  .facts { margin-top: 8px; } .muted { color: var(--text-2); font-size: 13px; }
  ul { margin: 8px 0 0; padding: 0; list-style: none; max-height: 160px; overflow: auto; }
  li { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px; }
</style>
